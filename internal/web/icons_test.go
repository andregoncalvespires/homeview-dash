package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// postMultipart posts form fields plus an optional "icon_file" to path.
func (e *env) postMultipart(c *http.Client, path string, fields url.Values, filename string, data []byte) (*http.Response, string) {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, vs := range fields {
		for _, v := range vs {
			mw.WriteField(k, v)
		}
	}
	if filename != "" {
		fw, _ := mw.CreateFormFile("icon_file", filename)
		fw.Write(data)
	}
	mw.Close()
	r, err := c.Post(e.srv.URL+path, mw.FormDataContentType(), &body)
	if err != nil {
		e.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r, string(b)
}

const goodSVG = `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8"><rect width="8" height="8" fill="#c33"/></svg>`

func TestIconUpload(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin, ana := e.login("admin@example.com"), e.login("ana@example.com")
	tok := e.csrf(admin)
	png := tinyPNG(t)
	form := func(name string) url.Values {
		return url.Values{"csrf": {tok}, "name": {name}, "internal_url": {"http://x.lan"}}
	}
	svcByName := func(name string) *store.Service {
		l, _ := e.st.ListServices(ctx)
		for i := range l {
			if l[i].Name == name {
				return &l[i]
			}
		}
		return nil
	}
	count := func() int {
		n := 0
		for id := int64(1); id < 200; id++ {
			if _, _, err := e.st.GetIcon(ctx, id); err == nil {
				n++
			}
		}
		return n
	}

	// A PNG becomes the service's icon and is served back with a locked-down policy.
	if r, _ := e.postMultipart(admin, "/admin/services/save", form("Um"), "logo.png", png); r.StatusCode != 303 {
		t.Fatalf("upload: %d", r.StatusCode)
	}
	um := svcByName("Um")
	id, ok := uploadIconID(um.Icon)
	if um == nil || !ok {
		t.Fatalf("icon = %+v", um)
	}
	r, body := e.get(admin, fmt.Sprintf("/icons/%d", id))
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") || body != string(png) {
		t.Errorf("serving: %d %v", r.StatusCode, r.Header)
	}
	if r, _ := e.get(ana, fmt.Sprintf("/icons/%d", id)); r.StatusCode != 200 { // every signed-in user needs it on the dashboard
		t.Errorf("regular user: %d", r.StatusCode)
	}
	if r, _ := e.get(e.client(), fmt.Sprintf("/icons/%d", id)); r.StatusCode != 303 {
		t.Errorf("anonymous: %d", r.StatusCode)
	}
	for _, bad := range []string{"/icons/0", "/icons/999", "/icons/abc", "/icons/-1"} {
		if r, _ := e.get(admin, bad); r.StatusCode != 404 {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}
	if _, b := e.get(ana, "/"); !strings.Contains(b, fmt.Sprintf(`src="/icons/%d"`, id)) {
		t.Errorf("dashboard does not show the uploaded icon:\n%s", b)
	}
	if _, b := e.get(admin, fmt.Sprintf("/admin/services?edit=%d", um.ID)); !strings.Contains(b, `class="iconprev"`) || !strings.Contains(b, `enctype="multipart/form-data"`) {
		t.Error("edit form lacks the preview or multipart encoding")
	}

	// The same image is stored once; SVG is accepted.
	e.postMultipart(admin, "/admin/services/save", form("Dois"), "again.png", png)
	if d := svcByName("Dois"); d == nil || d.Icon != um.Icon || count() != 1 {
		t.Errorf("identical image not shared: %+v, %d rows", d, count())
	}
	if r, _ := e.postMultipart(admin, "/admin/services/save", form("Svg"), "x.svg", []byte(goodSVG)); r.StatusCode != 303 {
		t.Fatalf("svg: %d", r.StatusCode)
	}
	svgID, _ := uploadIconID(svcByName("Svg").Icon)
	if r, _ := e.get(admin, fmt.Sprintf("/icons/%d", svgID)); r.Header.Get("Content-Type") != "image/svg+xml" {
		t.Errorf("svg type: %q", r.Header.Get("Content-Type"))
	}

	// Anything that is not a plain image is refused and leaves nothing behind.
	before := count()
	big := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, maxIcon+10)...)
	hostile := map[string][]byte{
		"html":      []byte("<html><script>alert(1)</script></html>"),
		"text":      []byte("just words"),
		"svg+js":    []byte(`<html><svg xmlns="http://www.w3.org/2000/svg"/></html>`),
		"entity":    []byte(`<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "aaaa">]><svg xmlns="http://www.w3.org/2000/svg">&a;</svg>`),
		"broken":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><g></svg>`),
		"fake png":  []byte("\x89PNG\r\n\x1a\nnot really"),
		"too large": big,
	}
	for name, data := range hostile {
		r, _ := e.postMultipart(admin, "/admin/services/save", form("Bad "+name), "bad", data)
		if r.StatusCode != 400 && r.StatusCode != 413 {
			t.Errorf("%s accepted: %d", name, r.StatusCode)
		}
		if svcByName("Bad "+name) != nil {
			t.Errorf("%s created a service", name)
		}
	}
	if count() != before {
		t.Errorf("rejected uploads left %d rows behind", count()-before)
	}

	// A validation error after a successful upload does not keep the image.
	noURL := form("Sem URL")
	noURL.Set("internal_url", "")
	other := []byte(strings.Replace(goodSVG, "#c33", "#33c", 1))
	if r, _ := e.postMultipart(admin, "/admin/services/save", noURL, "o.svg", other); r.StatusCode != 400 {
		t.Fatalf("invalid form: %d", r.StatusCode)
	}
	if count() != before {
		t.Error("image kept after a failed save")
	}

	// A reference to a missing upload, or a bad one, is refused.
	for _, icon := range []string{"upload:99999", "upload:abc", "upload:", "upload:-3"} {
		f := form("Ref " + icon)
		f.Set("icon", icon)
		if r, _ := e.post(admin, "/admin/services/save", f); r.StatusCode != 400 {
			t.Errorf("icon %q: %d", icon, r.StatusCode)
		}
	}
	// Keeping the text field as it is (edit without a new file) keeps the icon.
	f := form("Um")
	f.Set("id", fmt.Sprint(um.ID))
	f.Set("icon", um.Icon)
	if r, _ := e.post(admin, "/admin/services/save", f); r.StatusCode != 303 {
		t.Fatalf("edit: %d", r.StatusCode)
	}
	if svcByName("Um").Icon != um.Icon {
		t.Error("icon lost on edit")
	}

	// Replacing an icon frees the old one only when nobody else uses it.
	f = form("Um")
	f.Set("id", fmt.Sprint(um.ID))
	f.Set("icon", "🏠")
	e.post(admin, "/admin/services/save", f)
	if _, _, err := e.st.GetIcon(ctx, id); err != nil {
		t.Error("icon deleted while Dois still uses it")
	}
	dois := svcByName("Dois")
	e.post(admin, "/admin/services/delete", url.Values{"csrf": {tok}, "id": {fmt.Sprint(dois.ID)}})
	if _, _, err := e.st.GetIcon(ctx, id); err == nil {
		t.Error("unused icon not removed after its last service was deleted")
	}
	if _, _, err := e.st.GetIcon(ctx, svgID); err != nil {
		t.Error("an icon still in use was removed")
	}

	// Only admins may upload.
	if r, _ := e.postMultipart(ana, "/admin/services/save", url.Values{"csrf": {e.csrf(ana)}, "name": {"x"}, "internal_url": {"http://x.lan"}}, "a.png", png); r.StatusCode != 403 {
		t.Errorf("non-admin upload: %d", r.StatusCode)
	}
}

func TestIconUploadFromDiscovery(t *testing.T) {
	e, _ := discoveryEnv(t)
	ctx := context.Background()
	admin := e.login("admin@example.com")
	tok := e.csrf(admin)
	if r, _ := e.post(admin, "/admin/connections/save", validConnForm(tok)); r.StatusCode != 303 {
		t.Fatalf("connection: %d", r.StatusCode)
	}
	gs, _ := e.st.ListGuests(ctx)
	v := url.Values{"csrf": {tok}, "id": {fmt.Sprint(gs[0].ID)}, "name": {"Com ícone"}, "internal_url": {"http://192.168.1.10:8123"}}
	if r, _ := e.postMultipart(admin, "/admin/discovery/approve", v, "i.png", tinyPNG(t)); r.StatusCode != 303 {
		t.Fatalf("approve with icon: %d", r.StatusCode)
	}
	l, _ := e.st.ListServices(ctx)
	if len(l) != 1 || !strings.HasPrefix(l[0].Icon, uploadIconPrefix) || l[0].GuestID != gs[0].ID {
		t.Fatalf("service: %+v", l)
	}
	if _, b := e.get(admin, "/admin/discovery"); !strings.Contains(b, `name="icon_file"`) {
		t.Error("discovery form lacks the file field")
	}
}

func TestSniffIconAndRefs(t *testing.T) {
	for in, want := range map[string]bool{"upload:1": true, "upload:123456": true, "upload:0": false, "upload:": false, "upload:1a": false, "upload:-1": false, "upload:9999999999999999999999": false} {
		if _, ok := uploadIconID(in); ok != want {
			t.Errorf("uploadIconID(%q) = %v", in, ok)
		}
	}
	if validIcon("upload:abc") || !validIcon("upload:12") {
		t.Error("validIcon and upload references disagree")
	}
	if u, txt := resolveIcon("upload:12"); u != "/icons/12" || txt != "" {
		t.Errorf("resolveIcon upload: %q %q", u, txt)
	}
	if u, txt := resolveIcon("upload:zz"); u != "" || txt != "" {
		t.Errorf("dangling upload shows %q %q", u, txt)
	}
	if u, txt := resolveIcon("🏠"); u != "" || txt != "🏠" {
		t.Errorf("emoji: %q %q", u, txt)
	}
}
