package web

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
)

// pwaColor is the colour of the default (dark) theme, used by the install splash
// screen and the system bar until the page loads.
const pwaColor = "#0f1218"

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

// getManifest serves the web app manifest. It is public: browsers fetch it
// without credentials, and it holds nothing private.
func (s *Server) getManifest(w http.ResponseWriter, r *http.Request) {
	lang := s.lang(r)
	m := map[string]any{
		"id":               "/",
		"name":             "HomeView",
		"short_name":       "HomeView",
		"description":      i18n.T(lang, "pwa.desc"),
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"orientation":      "any",
		"background_color": pwaColor,
		"theme_color":      pwaColor,
		"lang":             lang,
		"categories":       []string{"utilities", "productivity"},
		"icons": []manifestIcon{
			{"/static/pwa/icon-192.png", "192x192", "image/png", "any"},
			{"/static/pwa/icon-512.png", "512x512", "image/png", "any"},
			{"/static/pwa/icon-maskable-512.png", "512x512", "image/png", "maskable"},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(m)
}

// getServiceWorker serves the worker from the site root so that its scope is the
// whole app. It must never be cached for long: browsers check it for updates.
func (s *Server) getServiceWorker(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(assets, "static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
