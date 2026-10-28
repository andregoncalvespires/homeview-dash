package web

import (
	"bytes"
	"encoding/json"
	"image/png"
	"strings"
	"testing"
)

func TestManifestIsPublicAndComplete(t *testing.T) {
	e := newEnv(t)
	r, b := e.get(e.client(), "/manifest.webmanifest") // no session
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/manifest+json") {
		t.Fatalf("manifest: %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(b), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "short_name", "start_url", "scope", "display", "theme_color", "background_color", "icons", "id"} {
		if raw[k] == nil {
			t.Errorf("manifest lacks %q", k)
		}
	}
	if raw["display"] != "standalone" || raw["start_url"] != "/" {
		t.Errorf("display/start_url = %v / %v", raw["display"], raw["start_url"])
	}
	// Every icon must exist, be a PNG of the declared size, and 192 + 512 + maskable must be present.
	icons, _ := raw["icons"].([]any)
	var any192, any512, maskable bool
	for _, i := range icons {
		ic := i.(map[string]any)
		src, sizes, purpose := ic["src"].(string), ic["sizes"].(string), ic["purpose"]
		ir, ib := e.get(e.client(), src)
		if ir.StatusCode != 200 || ir.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("icon %s: %d %s", src, ir.StatusCode, ir.Header.Get("Content-Type"))
		}
		img, err := png.Decode(bytes.NewReader([]byte(ib)))
		if err != nil {
			t.Fatalf("icon %s is not a PNG: %v", src, err)
		}
		if got := img.Bounds().Dx(); sizes != itoa(int64(got))+"x"+itoa(int64(got)) {
			t.Errorf("icon %s is %dpx, manifest says %s", src, got, sizes)
		}
		switch {
		case purpose == "maskable":
			maskable = true
		case sizes == "192x192":
			any192 = true
		case sizes == "512x512":
			any512 = true
		}
	}
	if !any192 || !any512 || !maskable {
		t.Errorf("icons: 192=%v 512=%v maskable=%v", any192, any512, maskable)
	}
}

func TestServiceWorker(t *testing.T) {
	e := newEnv(t)
	r, b := e.get(e.client(), "/sw.js")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/javascript") || r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("sw.js: %d %v", r.StatusCode, r.Header)
	}
	// It must stay away from private data: only bundled static files are cached,
	// never pages, the API or uploaded images.
	for _, must := range []string{`static\/`, "req.method !== 'GET'", "backgrounds", "mode === 'navigate'"} {
		if !strings.Contains(b, must) {
			t.Errorf("service worker lost %q", must)
		}
	}
	for _, never := range []string{"/api/", "/me/", "/icons/"} {
		if strings.Contains(b, never) {
			t.Errorf("service worker mentions %q: private data must not be cached", never)
		}
	}
}

func TestPagesAdvertiseTheApp(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/login"} {
		_, b := e.get(e.client(), path)
		for _, want := range []string{`rel="manifest" href="/manifest.webmanifest"`, `rel="apple-touch-icon"`, `name="theme-color"`, `viewport-fit=cover`} {
			if !strings.Contains(b, want) {
				t.Errorf("%s lacks %s", path, want)
			}
		}
	}
	_, b := e.get(e.login("ana@example.com"), "/")
	if !strings.Contains(b, `id="pwa-install"`) {
		t.Error("the gear menu has no install entry")
	}
	// The service worker is registered by the bundled script (CSP forbids inline code).
	if _, js := e.get(e.client(), "/static/app.js"); !strings.Contains(js, "serviceWorker.register('/sw.js'") {
		t.Error("app.js does not register the service worker")
	}
}
