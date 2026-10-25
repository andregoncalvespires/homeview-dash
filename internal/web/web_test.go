package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/auth"
	"github.com/andregoncalvespires/homeview-dash/internal/config"
	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

type fakeMail struct {
	mu    sync.Mutex
	codes []string // codes in send order
	tos   []string
}

var codeRe = regexp.MustCompile(`\b(\d{6})\b`)

func (f *fakeMail) Send(_ context.Context, to, _ string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tos = append(f.tos, to)
	f.codes = append(f.codes, codeRe.FindStringSubmatch(body)[1])
	return nil
}

func (f *fakeMail) last() string { f.mu.Lock(); defer f.mu.Unlock(); return f.codes[len(f.codes)-1] }
func (f *fakeMail) n() int       { f.mu.Lock(); defer f.mu.Unlock(); return len(f.codes) }

type env struct {
	t    *testing.T
	srv  *httptest.Server
	st   *store.MemStore
	mail *fakeMail
	auth *auth.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := store.NewMemStore()
	m := &fakeMail{}
	a := auth.New(st, m, []byte("0123456789abcdef0123456789abcdef"), auth.DefaultConfig())
	cfg := config.Config{DefaultLang: "pt-BR"}
	s, err := New(cfg, st, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.run = func(f func()) { f() }
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	e := &env{t: t, srv: srv, st: st, mail: m, auth: a}
	for _, u := range []struct {
		email, name string
		role        store.Role
	}{{"admin@example.com", "Admin", store.RoleAdmin}, {"ana@example.com", "Ana", store.RoleUser}} {
		if _, err := st.CreateUser(context.Background(), u.email, u.name, u.role); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// client returns an HTTP client with its own cookie jar that never follows redirects.
func (e *env) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func body(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *env) get(c *http.Client, path string, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		if strings.EqualFold(hdr[i], "Host") {
			req.Host = hdr[i+1]
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	if req.Host != "" && req.Host != req.URL.Host {
		// Cookie jars may key cookies by the request's Host header instead of the
		// URL, which would hide them when we fake a different Host. Send them
		// explicitly with a jar-less client so the test is independent of that.
		u, _ := url.Parse(e.srv.URL)
		for _, ck := range c.Jar.Cookies(u) {
			req.AddCookie(ck)
		}
		c = &http.Client{CheckRedirect: c.CheckRedirect}
	}
	r, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return r, body(e.t, r)
}

func (e *env) post(c *http.Client, path string, v url.Values) (*http.Response, string) {
	e.t.Helper()
	r, err := c.PostForm(e.srv.URL+path, v)
	if err != nil {
		e.t.Fatal(err)
	}
	return r, body(e.t, r)
}

// login performs the real e-mail code flow and returns a signed-in client.
func (e *env) login(email string) *http.Client {
	e.t.Helper()
	c := e.client()
	r, _ := e.post(c, "/login", url.Values{"email": {email}})
	if r.StatusCode != 303 || !strings.HasPrefix(r.Header.Get("Location"), "/verify?email=") {
		e.t.Fatalf("login: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	r, _ = e.post(c, "/verify", url.Values{"email": {email}, "code": {e.mail.last()}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/" {
		e.t.Fatalf("verify: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	return c
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (e *env) csrf(c *http.Client) string {
	e.t.Helper()
	_, b := e.get(c, "/devices")
	m := csrfRe.FindStringSubmatch(b)
	if m == nil {
		e.t.Fatal("no csrf token on page")
	}
	return m[1]
}

func TestHealthAndHeaders(t *testing.T) {
	e := newEnv(t)
	r, b := e.get(e.client(), "/healthz")
	if r.StatusCode != 200 || strings.TrimSpace(b) != "ok" {
		t.Fatalf("healthz: %d %q", r.StatusCode, b)
	}
	r, _ = e.get(e.client(), "/login")
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if r.Header.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "script-src 'self'") || r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("weak headers: %v", r.Header)
	}
}

func TestStaticAndNotFound(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/static/app.css", "/static/app.js", "/static/favicon.svg"} {
		if r, b := e.get(e.client(), p); r.StatusCode != 200 || b == "" {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}
	if r, _ := e.get(e.client(), "/nope"); r.StatusCode != 404 {
		t.Errorf("unknown path: %d", r.StatusCode)
	}
}

func TestRequiresLogin(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/devices", "/admin/services", "/admin/groups", "/admin/users"} {
		r, _ := e.get(e.client(), p)
		if r.StatusCode != 303 || r.Header.Get("Location") != "/login" {
			t.Errorf("%s: %d %q", p, r.StatusCode, r.Header.Get("Location"))
		}
	}
	r, _ := e.post(e.client(), "/admin/services/save", url.Values{"name": {"x"}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/login" {
		t.Errorf("anonymous POST: %d", r.StatusCode)
	}
}

func TestLoginFlow(t *testing.T) {
	e := newEnv(t)
	c := e.login("Ana@Example.com ")
	r, b := e.get(c, "/")
	if r.StatusCode != 200 || !strings.Contains(b, "Ana") {
		t.Fatalf("dashboard: %d", r.StatusCode)
	}
	u, _ := url.Parse(e.srv.URL)
	var found *http.Cookie
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == deviceCookie {
			found = ck
		}
	}
	if found == nil || len(found.Value) < 40 {
		t.Fatal("device cookie not set")
	}
	// Signed in users are sent away from /login.
	if r, _ := e.get(c, "/login"); r.StatusCode != 303 || r.Header.Get("Location") != "/" {
		t.Errorf("/login while signed in: %d", r.StatusCode)
	}
}

func TestSetCookieAttributes(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	e.post(c, "/login", url.Values{"email": {"ana@example.com"}})
	r, _ := e.post(c, "/verify", url.Values{"email": {"ana@example.com"}, "code": {e.mail.last()}})
	var ck *http.Cookie
	for _, x := range r.Cookies() {
		if x.Name == deviceCookie {
			ck = x
		}
	}
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.MaxAge < 86400*30 || ck.Path != "/" {
		t.Fatalf("cookie: %+v", ck)
	}
}

func TestActiveDeviceCookieIsRenewed(t *testing.T) {
	e := newEnv(t)
	c := e.login("ana@example.com")
	// Fresh login: last-seen is "now", so the cookie is re-issued once.
	r, _ := e.get(c, "/")
	renewed := func(r *http.Response) bool {
		for _, ck := range r.Cookies() {
			if ck.Name == deviceCookie && ck.MaxAge > 0 {
				return true
			}
		}
		return false
	}
	if !renewed(r) {
		t.Fatal("cookie not renewed right after sign-in")
	}
	// After more than an hour of silence the server refreshes last-seen and renews the cookie.
	now := time.Now()
	e.auth.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	if r, _ := e.get(c, "/"); !renewed(r) {
		t.Fatal("cookie not renewed after an idle hour")
	}
	// A request made right afterwards does not rewrite it.
	if r, _ := e.get(c, "/"); r.StatusCode != 200 {
		t.Fatal("device lost after renewal")
	}
}

func TestUnknownEmailLooksTheSame(t *testing.T) {
	e := newEnv(t)
	r, _ := e.post(e.client(), "/login", url.Values{"email": {"ghost@example.com"}})
	if r.StatusCode != 303 || !strings.HasPrefix(r.Header.Get("Location"), "/verify?email=") {
		t.Fatalf("got %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if e.mail.n() != 0 {
		t.Fatal("mail sent to unknown address")
	}
}

func TestInvalidEmailAndWrongCode(t *testing.T) {
	e := newEnv(t)
	r, b := e.post(e.client(), "/login", url.Values{"email": {"not-an-email"}})
	if r.StatusCode != 400 || !strings.Contains(b, "e-mail válido") {
		t.Errorf("invalid email: %d", r.StatusCode)
	}
	c := e.client()
	e.post(c, "/login", url.Values{"email": {"ana@example.com"}})
	wrong := "000000"
	if e.mail.last() == wrong {
		wrong = "000001"
	}
	r, b = e.post(c, "/verify", url.Values{"email": {"ana@example.com"}, "code": {wrong}})
	if r.StatusCode != 401 || !strings.Contains(b, "inválido ou expirado") {
		t.Errorf("wrong code: %d", r.StatusCode)
	}
	if r, _ := e.get(c, "/"); r.StatusCode != 303 {
		t.Error("wrong code granted access")
	}
}

func TestLinkLogin(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.st.UserByEmail(context.Background(), "admin@example.com")
	tok, err := e.auth.CreateLinkToken(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := e.client()
	if r, _ := e.get(c, "/auth/link?token="+url.QueryEscape(tok)); r.StatusCode != 303 || r.Header.Get("Location") != "/" {
		t.Fatalf("link: %d", r.StatusCode)
	}
	if r, _ := e.get(c, "/admin/users"); r.StatusCode != 200 {
		t.Fatalf("admin page after link login: %d", r.StatusCode)
	}
	if r, _ := e.get(e.client(), "/auth/link?token="+url.QueryEscape(tok)); r.StatusCode != 400 {
		t.Fatalf("reused link: %d", r.StatusCode)
	}
}

func TestPermissions(t *testing.T) {
	e := newEnv(t)
	user := e.login("ana@example.com")
	for _, p := range []string{"/admin/services", "/admin/groups", "/admin/users"} {
		if r, _ := e.get(user, p); r.StatusCode != 403 {
			t.Errorf("user GET %s: %d", p, r.StatusCode)
		}
	}
	token := e.csrf(user)
	for _, p := range []string{"/admin/services/save", "/admin/groups/save", "/admin/users/add", "/admin/users/delete"} {
		if r, _ := e.post(user, p, url.Values{"csrf": {token}, "name": {"x"}, "email": {"x@example.com"}, "role": {"admin"}, "internal_url": {"http://a"}}); r.StatusCode != 403 {
			t.Errorf("user POST %s: %d", p, r.StatusCode)
		}
	}
	if us, _ := e.st.ListUsers(context.Background()); len(us) != 2 {
		t.Fatalf("regular user changed users: %+v", us)
	}
	// The navigation of a regular user has no admin links.
	if _, b := e.get(user, "/"); strings.Contains(b, "/admin/") {
		t.Error("admin links shown to regular user")
	}
}

func TestCSRFRequired(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	form := url.Values{"name": {"Grafana"}, "internal_url": {"http://10.0.0.5:3000"}}
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 403 {
		t.Errorf("missing csrf: %d", r.StatusCode)
	}
	form.Set("csrf", "deadbeef")
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 403 {
		t.Errorf("bad csrf: %d", r.StatusCode)
	}
	if list, _ := e.st.ListServices(context.Background()); len(list) != 0 {
		t.Fatal("service created without a valid csrf token")
	}
	// A token from another device must not work either.
	other := e.login("admin@example.com")
	form.Set("csrf", e.csrf(other))
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 403 {
		t.Errorf("csrf from another device: %d", r.StatusCode)
	}
}

func TestAdminManagesCatalog(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)

	r, _ := e.post(admin, "/admin/groups/save", url.Values{"csrf": {tok}, "name": {"Proxmox › Docker01"}, "position": {"1"}})
	if r.StatusCode != 303 {
		t.Fatalf("save group: %d", r.StatusCode)
	}
	groups, _ := e.st.ListGroups(context.Background())
	if len(groups) != 1 {
		t.Fatalf("groups: %+v", groups)
	}
	gid := groups[0].ID

	form := url.Values{
		"csrf": {tok}, "name": {"Frigate"}, "description": {"NVR"}, "icon": {"📹"},
		"internal_url": {"http://10.0.0.5:5000"}, "external_url": {"https://frigate.example.com"},
		"group_id": {itoa(gid)}, "position": {"1"},
	}
	if r, _ := e.post(admin, "/admin/services/save", form); r.StatusCode != 303 {
		t.Fatalf("save service: %d", r.StatusCode)
	}
	list, _ := e.st.ListServices(context.Background())
	if len(list) != 1 || list[0].GroupID != gid {
		t.Fatalf("services: %+v", list)
	}

	// From the local network the internal URL is primary...
	_, b := e.get(admin, "/", "Host", "192.168.1.20:8080")
	if !strings.Contains(b, `class="card-main" href="http://10.0.0.5:5000"`) || !strings.Contains(b, `class="chip" href="https://frigate.example.com"`) {
		t.Errorf("internal context links wrong:\n%s", b)
	}
	if !strings.Contains(b, "Proxmox › Docker01") || !strings.Contains(b, "📹") {
		t.Error("group/icon not rendered")
	}
	// ...and from the public domain the external URL is primary.
	_, b = e.get(admin, "/", "Host", "dash.example.com")
	if !strings.Contains(b, `class="card-main" href="https://frigate.example.com"`) || !strings.Contains(b, `class="chip" href="http://10.0.0.5:5000"`) {
		t.Errorf("external context links wrong:\n%s", b)
	}

	// Edit page is prefilled; edit and delete work.
	if _, b := e.get(admin, "/admin/services?edit="+itoa(list[0].ID)); !strings.Contains(b, `value="Frigate"`) {
		t.Error("edit form not prefilled")
	}
	form.Set("id", itoa(list[0].ID))
	form.Set("name", "Frigate NVR")
	e.post(admin, "/admin/services/save", form)
	if sv, _ := e.st.GetService(context.Background(), list[0].ID); sv.Name != "Frigate NVR" {
		t.Errorf("edit not saved: %+v", sv)
	}
	e.post(admin, "/admin/services/delete", url.Values{"csrf": {tok}, "id": {itoa(list[0].ID)}})
	if l, _ := e.st.ListServices(context.Background()); len(l) != 0 {
		t.Error("service not deleted")
	}
	// Deleting the group works and the other pages render.
	e.post(admin, "/admin/groups/delete", url.Values{"csrf": {tok}, "id": {itoa(gid)}})
	if g, _ := e.st.ListGroups(context.Background()); len(g) != 0 {
		t.Error("group not deleted")
	}
}

func TestServiceValidation(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	bad := []url.Values{
		{"name": {"x"}, "internal_url": {"javascript:alert(1)"}},
		{"name": {"x"}, "external_url": {"data:text/html,<script>1</script>"}},
		{"name": {"x"}, "internal_url": {"ftp://host/file"}},
		{"name": {"x"}, "internal_url": {"http://"}},
		{"name": {"x"}, "internal_url": {"//evil.example.com"}},
		{"name": {"x"}}, // no URL
		{"name": {""}, "internal_url": {"http://a.example.com"}},
		{"name": {strings.Repeat("n", 81)}, "internal_url": {"http://a.example.com"}},
		{"name": {"x"}, "internal_url": {"http://a.example.com"}, "icon": {"<img src=x onerror=alert(1)>"}},
		{"name": {"x"}, "internal_url": {"http://a.example.com"}, "icon": {"javascript://x"}},
		{"name": {"x"}, "internal_url": {"http://a.example.com"}, "group_id": {"9999"}},
	}
	for i, v := range bad {
		v.Set("csrf", tok)
		if r, _ := e.post(admin, "/admin/services/save", v); r.StatusCode != 400 {
			t.Errorf("case %d accepted (%d): %v", i, r.StatusCode, v)
		}
	}
	if l, _ := e.st.ListServices(context.Background()); len(l) != 0 {
		t.Fatalf("invalid services were stored: %+v", l)
	}
}

func TestOutputIsEscaped(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	e.post(admin, "/admin/services/save", url.Values{
		"csrf": {tok}, "name": {`<script>alert("x")</script>`}, "description": {`"><img src=x onerror=alert(1)>`},
		"internal_url": {"http://a.example.com/?q=<b>"},
	})
	_, b := e.get(admin, "/")
	if strings.Contains(b, "<script>alert") || strings.Contains(b, "<img src=x") {
		t.Fatalf("unescaped user content:\n%s", b)
	}
	if !strings.Contains(b, "&lt;script&gt;") {
		t.Error("expected escaped name in output")
	}
}

func TestUsersAdmin(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	add := func(email, role string) *http.Response {
		r, _ := e.post(admin, "/admin/users/add", url.Values{"csrf": {tok}, "email": {email}, "name": {"New"}, "role": {role}})
		return r
	}
	if r := add("Bob@Example.com", "user"); r.Header.Get("Location") != "/admin/users?ok=msg.user_added" {
		t.Errorf("add: %q", r.Header.Get("Location"))
	}
	if r := add("bob@example.com", "user"); !strings.Contains(r.Header.Get("Location"), "err.user_exists") {
		t.Errorf("duplicate: %q", r.Header.Get("Location"))
	}
	if r := add("bad", "user"); !strings.Contains(r.Header.Get("Location"), "err.email") {
		t.Errorf("bad email: %q", r.Header.Get("Location"))
	}
	if r := add("eve@example.com", "root"); !strings.Contains(r.Header.Get("Location"), "err.name") {
		t.Errorf("bad role: %q", r.Header.Get("Location"))
	}
	// The new user can sign in.
	bob := e.login("bob@example.com")
	if r, _ := e.get(bob, "/"); r.StatusCode != 200 {
		t.Errorf("new user cannot sign in: %d", r.StatusCode)
	}
	// Flash messages render (pt-BR default and English).
	if _, b := e.get(admin, "/admin/users?err=err.user_self"); !strings.Contains(b, "excluir a si mesmo") {
		t.Error("flash not shown")
	}
	if _, b := e.get(admin, "/admin/users?err=<script>"); strings.Contains(b, "<script>") {
		t.Error("arbitrary flash reflected")
	}
	// An admin cannot delete themselves.
	self, _ := e.st.UserByEmail(context.Background(), "admin@example.com")
	r, _ := e.post(admin, "/admin/users/delete", url.Values{"csrf": {tok}, "id": {itoa(self.ID)}})
	if !strings.Contains(r.Header.Get("Location"), "err.user_self") {
		t.Errorf("self delete: %q", r.Header.Get("Location"))
	}
	// Deleting a user revokes their access.
	b, _ := e.st.UserByEmail(context.Background(), "bob@example.com")
	e.post(admin, "/admin/users/delete", url.Values{"csrf": {tok}, "id": {itoa(b.ID)}})
	if r, _ := e.get(bob, "/"); r.StatusCode != 303 {
		t.Errorf("deleted user still signed in: %d", r.StatusCode)
	}
}

func TestDevicesAndLogout(t *testing.T) {
	e := newEnv(t)
	phone := e.login("ana@example.com")
	laptop := e.login("ana@example.com")
	tok := e.csrf(laptop)

	_, b := e.get(laptop, "/devices")
	if strings.Count(b, "<tr>") < 3 || !strings.Contains(b, "este aparelho") {
		t.Errorf("devices page:\n%s", b)
	}
	me, _ := e.st.UserByEmail(context.Background(), "ana@example.com")
	devs, _ := e.st.ListDevices(context.Background(), me.ID)
	if len(devs) != 2 {
		t.Fatalf("devices: %+v", devs)
	}
	// Revoke every device except the laptop's own (the page only offers others).
	var phoneID int64
	for _, d := range devs {
		if !strings.Contains(b, `name="id" value="`+itoa(d.ID)+`"`) {
			continue
		}
		phoneID = d.ID
	}
	if phoneID == 0 {
		t.Fatal("no revoke button for the other device")
	}
	if r, _ := e.post(laptop, "/devices/revoke", url.Values{"csrf": {tok}, "id": {itoa(phoneID)}}); r.StatusCode != 303 {
		t.Fatalf("revoke: %d", r.StatusCode)
	}
	if r, _ := e.get(phone, "/"); r.StatusCode != 303 {
		t.Error("revoked device still works")
	}
	if r, _ := e.get(laptop, "/"); r.StatusCode != 200 {
		t.Error("current device broke")
	}
	// A user cannot revoke someone else's device.
	admin := e.login("admin@example.com")
	ad, _ := e.st.UserByEmail(context.Background(), "admin@example.com")
	adevs, _ := e.st.ListDevices(context.Background(), ad.ID)
	e.post(laptop, "/devices/revoke", url.Values{"csrf": {tok}, "id": {itoa(adevs[0].ID)}})
	if r, _ := e.get(admin, "/"); r.StatusCode != 200 {
		t.Error("revoked another user's device")
	}
	// Logout.
	if r, _ := e.post(laptop, "/logout", url.Values{"csrf": {tok}}); r.StatusCode != 303 || r.Header.Get("Location") != "/login" {
		t.Fatalf("logout: %d", r.StatusCode)
	}
	if r, _ := e.get(laptop, "/"); r.StatusCode != 303 {
		t.Error("still signed in after logout")
	}
}

func TestLanguage(t *testing.T) {
	e := newEnv(t)
	if _, b := e.get(e.client(), "/login"); !strings.Contains(b, "Entrar") {
		t.Error("default language is not pt-BR")
	}
	if _, b := e.get(e.client(), "/login", "Accept-Language", "en-US,en;q=0.9"); !strings.Contains(b, "Sign in") {
		t.Error("Accept-Language ignored")
	}
	c := e.client()
	if r, _ := e.get(c, "/lang?set=en&next=/login"); r.StatusCode != 303 || r.Header.Get("Location") != "/login" {
		t.Fatalf("lang switch: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if _, b := e.get(c, "/login"); !strings.Contains(b, "Sign in") {
		t.Error("language cookie ignored")
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/devices": "/devices", "/": "/", "": "/", "//evil.com": "/", "https://evil.com": "/",
		"/\\evil.com": "/", "/ok\r\nSet-Cookie: x=1": "/", "javascript:alert(1)": "/",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChooseLinks(t *testing.T) {
	both := store.Service{InternalURL: "http://in", ExternalURL: "https://ex"}
	if l := ChooseLinks(both, true); l.Primary != "http://in" || l.Secondary != "https://ex" || l.PrimaryIsExternal {
		t.Errorf("internal ctx: %+v", l)
	}
	if l := ChooseLinks(both, false); l.Primary != "https://ex" || l.Secondary != "http://in" || !l.PrimaryIsExternal {
		t.Errorf("external ctx: %+v", l)
	}
	if l := ChooseLinks(store.Service{ExternalURL: "https://ex"}, true); l.Primary != "https://ex" || l.Secondary != "" || !l.PrimaryIsExternal {
		t.Errorf("only external: %+v", l)
	}
	if l := ChooseLinks(store.Service{InternalURL: "http://in"}, false); l.Primary != "http://in" || l.Secondary != "" || l.PrimaryIsExternal {
		t.Errorf("only internal: %+v", l)
	}
	if l := ChooseLinks(store.Service{}, true); l.Primary != "" {
		t.Errorf("no urls: %+v", l)
	}
}

func TestIsInternalHost(t *testing.T) {
	yes := []string{"192.168.1.10:8080", "10.0.0.5", "172.16.3.4:80", "127.0.0.1:8080", "localhost:8080", "[::1]:8080",
		"homeview", "dash.lan", "nas.local:80", "x.home.arpa", "100.100.1.2", "169.254.1.1", "dash.casa.example.com"}
	no := []string{"dash.example.com", "dash.example.com:443", "8.8.8.8", "203.0.113.9:80", "", "172.32.0.1"}
	for _, h := range yes {
		if !IsInternalHost(h, []string{"dash.casa.example.com"}) {
			t.Errorf("%q should be internal", h)
		}
	}
	for _, h := range no {
		if IsInternalHost(h, []string{"dash.casa.example.com"}) {
			t.Errorf("%q should be external", h)
		}
	}
}

func TestValidators(t *testing.T) {
	for _, u := range []string{"http://a", "https://a.example.com:8443/path?x=1#y", "http://10.0.0.1:5000"} {
		if !validHTTPURL(u) {
			t.Errorf("rejected %q", u)
		}
	}
	for _, u := range []string{"", "a.example.com", "javascript:alert(1)", "http://", "https:///x", "ftp://x", "http://a b", "//x.com", "http://a\nb"} {
		if validHTTPURL(u) {
			t.Errorf("accepted %q", u)
		}
	}
	for _, i := range []string{"", "🏠", "HA", "https://x.example.com/i.png", "👨‍👩‍👧‍👦"} {
		if !validIcon(i) {
			t.Errorf("icon rejected %q", i)
		}
	}
	for _, i := range []string{"<b>", "javascript:alert(1)", "//x", "ftp://x", "http://", "a\x00b", strings.Repeat("x", 17), `"x`} {
		if validIcon(i) {
			t.Errorf("icon accepted %q", i)
		}
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0":                                              "Firefox · Linux",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/128.0 Mobile Safari/537.36":                       "Chrome · Android",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Version/17.5 Mobile/15E148 Safari/604.1": "Safari · iOS",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/128.0 Safari/537.36 Edg/128.0":                   "Edge · Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15":                   "Safari · macOS",
		"curl/8.0": "Browser",
	}
	for ua, want := range cases {
		if got := deviceLabel(ua); got != want {
			t.Errorf("deviceLabel(%q) = %q, want %q", ua, got, want)
		}
	}
}

// Every translation key used by a template must exist.
func TestTemplateKeysExist(t *testing.T) {
	re := regexp.MustCompile(`\{\{\s*t\s+\$?\.?Lang\s+"([^"]+)"`)
	reDyn := regexp.MustCompile(`"([a-z]+\.[a-z_.]+)"`)
	files, _ := assets.ReadDir("templates")
	n := 0
	for _, f := range files {
		b, _ := assets.ReadFile("templates/" + f.Name())
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			n++
			if !i18n.Has(m[1]) {
				t.Errorf("%s uses missing key %q", f.Name(), m[1])
			}
		}
		_ = reDyn
	}
	if n < 50 {
		t.Fatalf("only %d keys found; regexp is probably wrong", n)
	}
	// Keys passed from Go code.
	for _, k := range []string{"err.notfound", "err.forbidden", "err.csrf", "err.server", "err.badrequest", "err.link", "err.code", "err.email",
		"err.name", "err.url", "err.url_required", "err.icon", "err.description", "err.group_missing", "err.user_exists", "err.user_self",
		"err.last_admin", "msg.saved", "msg.deleted", "msg.revoked", "msg.user_added", "dash.ungrouped"} {
		if !i18n.Has(k) {
			t.Errorf("missing key %q", k)
		}
	}
}

// Every page renders in both languages for an admin.
func TestAllPagesRender(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	for _, lang := range []string{"pt-BR", "en"} {
		for _, p := range []string{"/", "/devices", "/admin/services", "/admin/groups", "/admin/users", "/nope"} {
			r, b := e.get(admin, p, "Accept-Language", lang)
			if (p == "/nope" && r.StatusCode != 404) || (p != "/nope" && r.StatusCode != 200) {
				t.Errorf("%s (%s): %d", p, lang, r.StatusCode)
			}
			if strings.Contains(b, "{{") || strings.Contains(b, "ZgotmplZ") || strings.Contains(b, "<no value>") {
				t.Errorf("%s (%s) has template artefacts", p, lang)
			}
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestBrokenEncodingIsRejected(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	broken := "Andr\xc3e Pires" // percent-encoded by url.Values, arrives as invalid UTF-8

	r, _ := e.post(admin, "/admin/users/add", url.Values{"csrf": {tok}, "email": {"bob@example.com"}, "name": {broken}, "role": {"user"}})
	if !strings.Contains(r.Header.Get("Location"), "err.name") {
		t.Errorf("user with broken name: %q", r.Header.Get("Location"))
	}
	r, _ = e.post(admin, "/admin/groups/save", url.Values{"csrf": {tok}, "name": {broken}})
	if !strings.Contains(r.Header.Get("Location"), "err.name") {
		t.Errorf("group with broken name: %q", r.Header.Get("Location"))
	}
	for _, v := range []url.Values{
		{"name": {broken}, "internal_url": {"http://a.example.com"}},
		{"name": {"ok"}, "description": {"x\xc3"}, "internal_url": {"http://a.example.com"}},
		{"name": {"ok"}, "internal_url": {"http://a.example.com/\xc3"}},
		{"name": {"ok"}, "internal_url": {"http://a.example.com"}, "icon": {"\xc3"}},
		{"name": {"line\nbreak"}, "internal_url": {"http://a.example.com"}},
	} {
		v.Set("csrf", tok)
		if r, _ := e.post(admin, "/admin/services/save", v); r.StatusCode != 400 {
			t.Errorf("accepted %q (%d)", v, r.StatusCode)
		}
	}
	if us, _ := e.st.ListUsers(context.Background()); len(us) != 2 {
		t.Errorf("users: %+v", us)
	}
	if g, _ := e.st.ListGroups(context.Background()); len(g) != 0 {
		t.Errorf("groups: %+v", g)
	}
	if l, _ := e.st.ListServices(context.Background()); len(l) != 0 {
		t.Errorf("services: %+v", l)
	}

	// Correctly encoded accents are stored and shown intact.
	e.post(admin, "/admin/users/add", url.Values{"csrf": {tok}, "email": {"bob@example.com"}, "name": {"André Pires"}, "role": {"user"}})
	bob := e.login("bob@example.com")
	if _, b := e.get(bob, "/"); !strings.Contains(b, "André Pires") || strings.Contains(b, "�") {
		t.Errorf("accented name not rendered intact:\n%s", b)
	}
	if r, _ := e.get(bob, "/"); !strings.Contains(r.Header.Get("Content-Type"), "charset=utf-8") {
		t.Errorf("content type: %q", r.Header.Get("Content-Type"))
	}
}
