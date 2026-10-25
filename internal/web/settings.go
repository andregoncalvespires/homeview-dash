package web

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoders used by image.DecodeConfig
	_ "image/png"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

const (
	maxBackground    = 3 << 20 // bytes
	maxBackgroundPx  = 10000   // per side
	uploadBodyBudget = maxBackground + 512<<10
)

// accentPresets are the ready-made accent colours offered on the settings page.
var accentPresets = []struct{ ID, Hex string }{
	{"blue", "#2563eb"}, {"teal", "#0d9488"}, {"green", "#16a34a"}, {"orange", "#ea580c"},
	{"red", "#dc2626"}, {"pink", "#db2777"}, {"purple", "#7c3aed"}, {"slate", "#475569"},
}

type presetView struct{ ID, Hex string }

type settingsData struct {
	Prefs    store.Prefs
	Presets  []presetView
	Selected string // "", a preset ID or "custom"
	Custom   string // colour shown in the custom picker
	BgURL    string
}

func presetFor(accent string) (selected, custom string) {
	custom = "#2563eb"
	if accent == "" {
		return "", custom
	}
	for _, p := range accentPresets {
		if p.Hex == accent {
			return p.ID, custom
		}
	}
	return "custom", accent
}

func (s *Server) loadSettings(sess *session, p store.Prefs) settingsData {
	d := settingsData{Prefs: p}
	for _, a := range accentPresets {
		d.Presets = append(d.Presets, presetView{a.ID, a.Hex})
	}
	d.Selected, d.Custom = presetFor(p.Accent)
	d.BgURL = backgroundURL(p)
	return d
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, sess *session) {
	p, err := s.st.GetPrefs(r.Context(), sess.User.ID)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "settings", http.StatusOK, &page{TitleKey: "settings.title", Data: s.loadSettings(sess, p)})
}

func (s *Server) postAppearance(w http.ResponseWriter, r *http.Request, sess *session) {
	p, err := s.st.GetPrefs(r.Context(), sess.User.ID)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	theme := r.PostFormValue("theme")
	dim, derr := strconv.Atoi(r.PostFormValue("bg_dim"))
	accent := ""
	switch sel := r.PostFormValue("accent_preset"); sel {
	case "":
	case "custom":
		accent = r.PostFormValue("accent_custom")
	default:
		found := false
		for _, a := range accentPresets {
			if a.ID == sel {
				accent, found = a.Hex, true
			}
		}
		if !found {
			s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
			return
		}
	}
	accent, accentOK := store.NormalizeAccent(accent)
	if !store.ValidTheme(theme) || derr != nil || dim < 0 || dim > 80 || !accentOK {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	p.Theme, p.Accent, p.BgDim = theme, accent, dim
	if err := s.st.SavePrefs(r.Context(), p); err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/settings?ok=msg.prefs_saved", http.StatusSeeOther)
}

// sniffImage returns the content type of an allowed background image after
// checking that it really is one. SVG is deliberately not accepted.
func sniffImage(data []byte) (string, error) {
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxBackgroundPx || cfg.Height > maxBackgroundPx {
			return "", errors.New("unreadable or oversized image")
		}
		return ct, nil
	case "image/webp":
		return ct, nil
	default:
		return "", fmt.Errorf("unsupported type %q", ct)
	}
}

func (s *Server) postBackground(w http.ResponseWriter, r *http.Request, sess *session) {
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/settings?err=err.upload_none", http.StatusSeeOther)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBackground+1))
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	if len(data) == 0 {
		http.Redirect(w, r, "/settings?err=err.upload_none", http.StatusSeeOther)
		return
	}
	if len(data) > maxBackground {
		http.Redirect(w, r, "/settings?err=err.upload_size", http.StatusSeeOther)
		return
	}
	ct, err := sniffImage(data)
	if err != nil {
		http.Redirect(w, r, "/settings?err=err.upload_type", http.StatusSeeOther)
		return
	}
	if err := s.st.SetBackground(r.Context(), sess.User.ID, ct, data, time.Now()); err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/settings?ok=msg.bg_saved", http.StatusSeeOther)
}

func (s *Server) deleteBackground(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := s.st.ClearBackground(r.Context(), sess.User.ID); err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/settings?ok=msg.bg_removed", http.StatusSeeOther)
}

func backgroundURL(p store.Prefs) string {
	if !p.HasBg {
		return ""
	}
	return fmt.Sprintf("/me/background?v=%d-%d", p.UserID, p.BgVersion)
}

// themeVersion changes whenever the generated stylesheet would, so it can be
// cached for a long time.
func themeVersion(p store.Prefs) string {
	return fmt.Sprintf("%d-%s-%d-%d", p.UserID, strings.TrimPrefix(p.Accent, "#"), p.BgDim, p.BgVersion)
}

// contrastText picks black or white text for a #rrggbb background.
func contrastText(hex string) string {
	if len(hex) != 7 {
		return "#ffffff"
	}
	var c [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return "#ffffff"
		}
		f := float64(v) / 255
		if f <= 0.03928 {
			c[i] = f / 12.92
		} else {
			c[i] = math.Pow((f+0.055)/1.055, 2.4)
		}
	}
	lum := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	// White text needs luminance below ~0.18 for a 4.5:1 ratio.
	if lum > 0.18 {
		return "#0b1020"
	}
	return "#ffffff"
}

// getThemeCSS serves the signed-in user's stylesheet: accent colour and, when
// there is one, the background image. Everything else lives in app.css.
func (s *Server) getThemeCSS(w http.ResponseWriter, r *http.Request, sess *session) {
	p, err := s.st.GetPrefs(r.Context(), sess.User.ID)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	var b strings.Builder
	b.WriteString("/* generated per user */\n")
	if p.Accent != "" {
		// html:root[data-theme] outranks the dark-mode defaults in app.css.
		fmt.Fprintf(&b, "html:root[data-theme]{--accent:%s;--accent-text:%s;}\n", p.Accent, contrastText(p.Accent))
	}
	if p.HasBg {
		fmt.Fprintf(&b, "body.has-bg{background-image:linear-gradient(color-mix(in srgb,var(--bg) %d%%,transparent),color-mix(in srgb,var(--bg) %d%%,transparent)),url(%s);"+
			"background-size:cover;background-position:center;background-attachment:fixed;}\n", p.BgDim, p.BgDim, backgroundURL(p))
		b.WriteString(".has-bg .panel,.has-bg .card,.has-bg .tablewrap,.has-bg .guests>li{background:color-mix(in srgb,var(--panel) 86%,transparent);" +
			"-webkit-backdrop-filter:blur(8px);backdrop-filter:blur(8px);}\n")
		b.WriteString(".has-bg .top,.has-bg footer{background:color-mix(in srgb,var(--panel) 70%,transparent);" +
			"-webkit-backdrop-filter:blur(8px);backdrop-filter:blur(8px);}\n")
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = io.WriteString(w, b.String())
}

func (s *Server) getBackground(w http.ResponseWriter, r *http.Request, sess *session) {
	ct, data, err := s.st.GetBackground(r.Context(), sess.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(data)
}
