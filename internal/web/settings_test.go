package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.RGBA{200, 30, 30, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeader fakes a PNG whose header claims the given size (nothing else is valid).
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	chunk := append([]byte("IHDR"), ihdr...)
	binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func (e *env) upload(c *http.Client, csrf, filename string, data []byte) *http.Response {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if csrf != "" {
		mw.WriteField("csrf", csrf)
	}
	if filename != "" {
		fw, _ := mw.CreateFormFile("file", filename)
		fw.Write(data)
	}
	mw.Close()
	r, err := c.Post(e.srv.URL+"/settings/background", mw.FormDataContentType(), &body)
	if err != nil {
		e.t.Fatal(err)
	}
	r.Body.Close()
	return r
}

func TestAppearance(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if r, _ := e.get(e.client(), "/settings"); r.StatusCode != 303 {
		t.Errorf("anonymous /settings: %d", r.StatusCode)
	}
	if r, _ := e.get(e.client(), "/me/theme.css"); r.StatusCode != 303 {
		t.Errorf("anonymous theme.css: %d", r.StatusCode)
	}

	ana := e.login("ana@example.com")
	tok := e.csrf(ana)
	if r, b := e.get(ana, "/settings"); r.StatusCode != 200 || !strings.Contains(b, `href="/settings"`) || !strings.Contains(b, `name="accent_preset"`) || !strings.Contains(b, `data-theme="dark"`) {
		t.Fatalf("settings page: %d", r.StatusCode)
	}

	save := func(v url.Values) *http.Response {
		v.Set("csrf", tok)
		r, _ := e.post(ana, "/settings/appearance", v)
		return r
	}
	// Preset colour + forced dark theme.
	if r := save(url.Values{"theme": {"dark"}, "accent_preset": {"purple"}, "bg_dim": {"50"}}); r.StatusCode != 303 {
		t.Fatalf("save: %d", r.StatusCode)
	}
	u, _ := e.st.UserByEmail(ctx, "ana@example.com")
	if p, _ := e.st.GetPrefs(ctx, u.ID); p.Theme != "dark" || p.Accent != "#7c3aed" || p.BgDim != 50 {
		t.Fatalf("prefs: %+v", p)
	}
	_, b := e.get(ana, "/")
	if !strings.Contains(b, `<html lang="pt-BR" data-theme="dark">`) || !strings.Contains(b, `/me/theme.css?v=`) {
		t.Errorf("dashboard does not apply the theme:\n%s", b)
	}
	r, css := e.get(ana, "/me/theme.css")
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "text/css") || !strings.Contains(css, "--accent:#7c3aed;--accent-text:#ffffff;") ||
		!strings.Contains(css, "url(/static/backgrounds/estrelas.webp)") || !strings.Contains(r.Header.Get("Cache-Control"), "private") {
		t.Errorf("theme.css (%s): %s", r.Header.Get("Cache-Control"), css)
	}
	// The settings page reflects the saved choice.
	if _, b := e.get(ana, "/settings"); !strings.Contains(b, `value="purple" checked`) || !strings.Contains(b, `value="dark" checked`) {
		t.Errorf("saved choices not shown:\n%s", b)
	}

	// Custom colour: light colours get dark text.
	save(url.Values{"theme": {"light"}, "accent_preset": {"custom"}, "accent_custom": {"#FFEE00"}, "bg_dim": {"10"}})
	if _, css := e.get(ana, "/me/theme.css"); !strings.Contains(css, "--accent:#ffee00;--accent-text:#0b1020;") {
		t.Errorf("custom accent css: %s", css)
	}
	// Back to the default colour: no override at all.
	save(url.Values{"theme": {"auto"}, "accent_preset": {""}, "bg_dim": {"35"}})
	if _, css := e.get(ana, "/me/theme.css"); strings.Contains(css, "--accent") {
		t.Errorf("default accent should not be overridden: %s", css)
	}

	// Bad input is refused and changes nothing.
	for name, v := range map[string]url.Values{
		"theme":   {"theme": {"neon"}, "bg_dim": {"35"}},
		"preset":  {"theme": {"dark"}, "accent_preset": {"hotpink"}, "bg_dim": {"35"}},
		"custom":  {"theme": {"dark"}, "accent_preset": {"custom"}, "accent_custom": {"red;}body{display:none"}, "bg_dim": {"35"}},
		"dim":     {"theme": {"dark"}, "bg_dim": {"99"}},
		"dim nan": {"theme": {"dark"}, "bg_dim": {"x"}},
	} {
		if r := save(v); r.StatusCode != 400 {
			t.Errorf("%s: %d", name, r.StatusCode)
		}
	}
	if p, _ := e.st.GetPrefs(ctx, u.ID); p.Theme != "auto" || p.Accent != "" {
		t.Errorf("bad input changed prefs: %+v", p)
	}
	if r, _ := e.post(ana, "/settings/appearance", url.Values{"theme": {"dark"}, "bg_dim": {"35"}}); r.StatusCode != 403 {
		t.Errorf("no csrf: %d", r.StatusCode)
	}

	// Preferences are per user.
	admin := e.login("admin@example.com")
	save(url.Values{"theme": {"light"}, "accent_preset": {"red"}, "bg_dim": {"35"}})
	if _, b := e.get(admin, "/"); !strings.Contains(b, `data-theme="dark"`) { // the default for new users
		t.Error("another user's theme leaked")
	}
	if _, css := e.get(admin, "/me/theme.css"); strings.Contains(css, "--accent") {
		t.Error("another user's accent leaked")
	}
}

func TestBackgroundUpload(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ana := e.login("ana@example.com")
	tok := e.csrf(ana)
	u, _ := e.st.UserByEmail(ctx, "ana@example.com")
	good := tinyPNG(t)

	if r := e.upload(ana, tok, "bg.png", good); r.StatusCode != 303 || r.Header.Get("Location") != "/settings?ok=msg.bg_saved" {
		t.Fatalf("upload: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	p, _ := e.st.GetPrefs(ctx, u.ID)
	if !p.HasBg {
		t.Fatal("background not stored")
	}
	r, data := e.get(ana, "/me/background")
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || data != string(good) ||
		!strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("serving: %d %v", r.StatusCode, r.Header)
	}
	_, css := e.get(ana, "/me/theme.css")
	if !strings.Contains(css, "body.has-bg{") || !strings.Contains(css, "url(/me/background?v=") || !strings.Contains(css, "35%") {
		t.Errorf("theme.css lacks the background: %s", css)
	}
	_, b := e.get(ana, "/")
	if !strings.Contains(b, `<body class="has-bg">`) {
		t.Error("body class missing")
	}
	if _, b := e.get(ana, "/settings"); !strings.Contains(b, `class="bgprev"`) || !strings.Contains(b, "/settings/background/delete") {
		t.Error("settings page lacks preview/remove")
	}
	// Strictly private: nobody else gets it, and anonymous visitors are sent to sign in.
	admin := e.login("admin@example.com")
	if r, _ := e.get(admin, "/me/background"); r.StatusCode != 404 {
		t.Errorf("another user can read it: %d", r.StatusCode)
	}
	if r, _ := e.get(e.client(), "/me/background"); r.StatusCode != 303 {
		t.Errorf("anonymous: %d", r.StatusCode)
	}

	// Rejected uploads leave the stored image alone.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	big := append(append([]byte{}, good...), make([]byte, 3<<20)...) // valid PNG header, over 3 MB
	cases := map[string]struct {
		name string
		data []byte
		loc  string
	}{
		"svg":          {"x.svg", svg, "err.upload_type"},
		"html as png":  {"x.png", []byte("<html><script>alert(1)</script></html>"), "err.upload_type"},
		"gif":          {"x.gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), "err.upload_type"},
		"truncated":    {"x.png", good[:20], "err.upload_type"},
		"huge pixels":  {"x.png", pngHeader(20000, 20000), "err.upload_type"},
		"over 3MB":     {"x.png", big, "err.upload_size"},
		"empty":        {"x.png", nil, "err.upload_none"},
		"no file part": {"", nil, "err.upload_none"},
	}
	for name, c := range cases {
		r := e.upload(ana, tok, c.name, c.data)
		if r.StatusCode != 303 || r.Header.Get("Location") != "/settings?err="+c.loc {
			t.Errorf("%s: %d %q", name, r.StatusCode, r.Header.Get("Location"))
		}
	}
	if r := e.upload(ana, tok, "x.png", make([]byte, 5<<20)); r.StatusCode != 413 {
		t.Errorf("body over the budget: %d", r.StatusCode)
	}
	if r := e.upload(ana, "", "bg.png", good); r.StatusCode != 403 {
		t.Errorf("no csrf: %d", r.StatusCode)
	}
	if _, d, _ := e.st.GetBackground(ctx, u.ID); string(d) != string(good) {
		t.Error("rejected upload replaced the image")
	}

	// JPEG and WebP are accepted by their signature.
	jpg := bytes.NewBuffer(nil)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := encodeJPEG(jpg, img); err != nil {
		t.Fatal(err)
	}
	if r := e.upload(ana, tok, "a.jpg", jpg.Bytes()); r.StatusCode != 303 || strings.Contains(r.Header.Get("Location"), "err") {
		t.Errorf("jpeg: %q", r.Header.Get("Location"))
	}
	if ct, _, _ := e.st.GetBackground(ctx, u.ID); ct != "image/jpeg" {
		t.Errorf("content type %q", ct)
	}
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 40)...)
	if r := e.upload(ana, tok, "a.webp", webp); r.StatusCode != 303 || strings.Contains(r.Header.Get("Location"), "err") {
		t.Errorf("webp: %q", r.Header.Get("Location"))
	}

	// Removal.
	if r, _ := e.post(ana, "/settings/background/delete", url.Values{"csrf": {tok}}); r.Header.Get("Location") != "/settings?ok=msg.bg_removed" {
		t.Errorf("remove: %q", r.Header.Get("Location"))
	}
	if r, _ := e.get(ana, "/me/background"); r.StatusCode != 404 {
		t.Errorf("after removal: %d", r.StatusCode)
	}
	if _, css := e.get(ana, "/me/theme.css"); strings.Contains(css, "has-bg") {
		t.Error("css still references the removed image")
	}
}

func TestBundledBackgrounds(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ana := e.login("ana@example.com")
	tok := e.csrf(ana)
	u, _ := e.st.UserByEmail(ctx, "ana@example.com")
	pick := func(id string) *http.Response {
		r, _ := e.post(ana, "/settings/background/preset", url.Values{"csrf": {tok}, "preset": {id}})
		return r
	}

	// The default applies from the start and the page offers every wallpaper.
	if _, b := e.get(ana, "/"); !strings.Contains(b, `<body class="has-bg">`) {
		t.Error("default wallpaper not applied")
	}
	_, page := e.get(ana, "/settings")
	for _, want := range []string{`value="estrelas" aria-pressed="true"`, `value="grafite" aria-pressed="false"`, `/static/backgrounds/dunas-thumb.webp`, "Céu estrelado"} {
		if !strings.Contains(page, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
	if r, _ := e.get(ana, "/static/backgrounds/estrelas.webp"); r.StatusCode != 200 {
		t.Errorf("wallpaper not served: %d", r.StatusCode)
	}

	// Choosing one changes the stylesheet (and its cache key).
	_, before := e.get(ana, "/")
	if r := pick("dunas"); r.StatusCode != 303 || r.Header.Get("Location") != "/settings?ok=msg.bg_saved" {
		t.Fatalf("pick: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if _, css := e.get(ana, "/me/theme.css"); !strings.Contains(css, "url(/static/backgrounds/dunas.webp)") {
		t.Errorf("css: %s", css)
	}
	if _, after := e.get(ana, "/"); after == before {
		t.Error("theme.css cache key did not change")
	}

	// An upload replaces it; picking one again drops the upload.
	if r := e.upload(ana, tok, "bg.png", tinyPNG(t)); r.StatusCode != 303 {
		t.Fatal("upload failed")
	}
	if p, _ := e.st.GetPrefs(ctx, u.ID); !p.HasBg || p.BgPreset != "" {
		t.Errorf("after upload: %+v", p)
	}
	pick("floresta")
	if p, _ := e.st.GetPrefs(ctx, u.ID); p.HasBg || p.BgPreset != "floresta" {
		t.Errorf("after preset: %+v", p)
	}

	// "No background", and hostile ids.
	pick("")
	if _, css := e.get(ana, "/me/theme.css"); strings.Contains(css, "has-bg") {
		t.Error("background still applied after choosing none")
	}
	if _, b := e.get(ana, "/"); strings.Contains(b, "has-bg") {
		t.Error("body still has-bg")
	}
	for _, bad := range []string{"../../etc/passwd", "nope", "dunas.webp", "x);}body{display:none"} {
		if r := pick(bad); r.StatusCode != 400 {
			t.Errorf("%q: %d", bad, r.StatusCode)
		}
	}
	if r, _ := e.post(ana, "/settings/background/preset", url.Values{"preset": {"dunas"}}); r.StatusCode != 403 {
		t.Errorf("no csrf: %d", r.StatusCode)
	}
	if p, _ := e.st.GetPrefs(ctx, u.ID); p.BgPreset != "" {
		t.Errorf("rejected input changed prefs: %+v", p)
	}
}

func TestContrastText(t *testing.T) {
	dark := []string{"#2563eb", "#7c3aed", "#dc2626", "#000000", "#475569"}
	light := []string{"#ffee00", "#ffffff", "#a3e635", "#fde68a", "#0d9488", "#16a34a"} // white on these would be under 4.5:1
	for _, c := range dark {
		if contrastText(c) != "#ffffff" {
			t.Errorf("%s should take white text", c)
		}
	}
	for _, c := range light {
		if contrastText(c) != "#0b1020" {
			t.Errorf("%s should take dark text", c)
		}
	}
	if contrastText("bogus") != "#ffffff" {
		t.Error("fallback")
	}
}

func encodeJPEG(w *bytes.Buffer, img image.Image) error { return jpeg.Encode(w, img, nil) }
