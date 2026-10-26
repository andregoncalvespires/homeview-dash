// Package web is HomeView's HTTP layer: server-rendered pages, sessions via a
// device cookie, CSRF protection and the admin screens.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/auth"
	"github.com/andregoncalvespires/homeview-dash/internal/config"
	"github.com/andregoncalvespires/homeview-dash/internal/discovery"
	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

const (
	deviceCookie = "homeview_device"
	langCookie   = "hv_lang"
)

// pageFiles are the templates rendered inside the shared layout.
var pageFiles = []string{
	"login", "verify", "dashboard", "devices", "settings",
	"admin_services", "admin_groups", "admin_users", "admin_connections", "admin_discovery", "error",
}

// Server wires everything together.
type Server struct {
	cfg    config.Config
	st     store.Store
	auth   *auth.Service
	disc   *discovery.Service // optional; see SetDiscovery
	log    *slog.Logger
	pages  map[string]*template.Template
	static http.Handler
	// run executes background work (sending e-mail). Tests replace it with a
	// synchronous version.
	run func(func())
}

// New builds a Server.
func New(cfg config.Config, st store.Store, a *auth.Service, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg: cfg, st: st, auth: a, log: log,
		pages: map[string]*template.Template{},
		run:   func(f func()) { go f() },
	}
	funcs := template.FuncMap{
		"t":  i18n.T,
		"dt": formatTime,
		// dict lets a template pass several named values to a sub-template.
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
	}
	for _, name := range pageFiles {
		t, err := template.New(name).Funcs(funcs).ParseFS(assets, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		s.pages[name] = t
	}
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	s.static = http.StripPrefix("/static/", http.FileServerFS(sub))
	return s, nil
}

// Handler returns the full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		s.static.ServeHTTP(w, r)
	})

	// Public.
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.HandleFunc("GET /verify", s.getVerify)
	mux.HandleFunc("POST /verify", s.postVerify)
	mux.HandleFunc("GET /auth/link", s.getLink)
	mux.HandleFunc("GET /lang", s.getLang)

	// Signed-in users.
	mux.HandleFunc("GET /{$}", s.require(false, s.dashboard))
	mux.HandleFunc("POST /logout", s.require(false, s.logout))
	mux.HandleFunc("GET /settings", s.require(false, s.getSettings))
	mux.HandleFunc("POST /settings/appearance", s.require(false, s.postAppearance))
	mux.HandleFunc("POST /settings/background", s.requireLimit(false, uploadBodyBudget, s.postBackground))
	mux.HandleFunc("POST /admin/layout", s.require(true, s.postLayout))
	mux.HandleFunc("POST /settings/background/preset", s.require(false, s.postBgPreset))
	mux.HandleFunc("POST /settings/background/delete", s.require(false, s.deleteBackground))
	mux.HandleFunc("GET /me/theme.css", s.require(false, s.getThemeCSS))
	mux.HandleFunc("GET /me/background", s.require(false, s.getBackground))
	mux.HandleFunc("GET /devices", s.require(false, s.getDevices))
	mux.HandleFunc("POST /devices/revoke", s.require(false, s.revokeDevice))

	// Administrators.
	mux.HandleFunc("GET /admin/services", s.require(true, s.getAdminServices))
	mux.HandleFunc("POST /admin/services/save", s.requireLimit(true, uploadIconBudget, s.saveService))
	mux.HandleFunc("GET /icons/{id}", s.require(false, s.getIcon))
	mux.HandleFunc("POST /admin/services/delete", s.require(true, s.deleteService))
	mux.HandleFunc("GET /admin/groups", s.require(true, s.getAdminGroups))
	mux.HandleFunc("POST /admin/groups/save", s.require(true, s.saveGroup))
	mux.HandleFunc("POST /admin/groups/delete", s.require(true, s.deleteGroup))
	mux.HandleFunc("GET /admin/discovery", s.require(true, s.getAdminDiscovery))
	mux.HandleFunc("POST /admin/discovery/sync", s.require(true, s.syncDiscovery))
	mux.HandleFunc("POST /admin/discovery/approve", s.requireLimit(true, uploadIconBudget, s.approveGuest))
	mux.HandleFunc("POST /admin/discovery/ignore", s.require(true, s.setGuestState(store.GuestIgnored, "msg.guest_ignored")))
	mux.HandleFunc("POST /admin/discovery/restore", s.require(true, s.setGuestState(store.GuestNew, "msg.guest_restored")))
	mux.HandleFunc("POST /admin/discovery/forget", s.require(true, s.forgetGuest))
	mux.HandleFunc("GET /admin/connections", s.require(true, s.getAdminConnections))
	mux.HandleFunc("POST /admin/connections/save", s.require(true, s.saveConnection))
	mux.HandleFunc("POST /admin/connections/delete", s.require(true, s.deleteConnection))
	mux.HandleFunc("GET /admin/users", s.require(true, s.getAdminUsers))
	mux.HandleFunc("POST /admin/users/add", s.require(true, s.addUser))
	mux.HandleFunc("POST /admin/users/delete", s.require(true, s.deleteUser))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sess, _ := s.session(r)
		s.renderError(w, r, sess, http.StatusNotFound, "err.notfound")
	})
	return s.logRequests(secureHeaders(mux))
}

// ---- middleware ----

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' http: https: data:; "+
			"style-src 'self'; script-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
			return
		}
		// The query string is never logged: it may hold e-mail addresses or tokens.
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds())
	})
}

// session is the authenticated context of a request.
type session struct {
	User   *store.User
	Device *store.Device
	Token  string // raw cookie value, to renew the cookie
}

func (s *Server) session(r *http.Request) (*session, bool) {
	c, err := r.Cookie(deviceCookie)
	if err != nil {
		return nil, false
	}
	u, d, err := s.auth.Authenticate(r.Context(), c.Value)
	if err != nil {
		return nil, false
	}
	return &session{User: u, Device: d, Token: c.Value}, true
}

// require wraps a handler with authentication, optional admin check, request
// size limits and CSRF validation for POSTs.
func (s *Server) require(admin bool, h func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return s.requireLimit(admin, 1<<20, h)
}

// requireLimit is require with a custom request body limit (uploads).
func (s *Server) requireLimit(admin bool, limit int64, h func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.session(r)
		if !ok {
			s.clearCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if admin && !sess.User.IsAdmin() {
			s.renderError(w, r, sess, http.StatusForbidden, "err.forbidden")
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					var tooBig *http.MaxBytesError
					if errors.As(err, &tooBig) {
						s.renderError(w, r, sess, http.StatusRequestEntityTooLarge, "err.upload_size")
					} else {
						s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
					}
					return
				}
				defer r.MultipartForm.RemoveAll()
			} else if err := r.ParseForm(); err != nil {
				s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
				return
			}
			if !s.auth.CheckCSRF(sess.Device, r.PostFormValue("csrf")) {
				s.renderError(w, r, sess, http.StatusForbidden, "err.csrf")
				return
			}
		}
		// Authenticate refreshes last-seen at most hourly; when that just happened,
		// renew the cookie too so active devices stay signed in.
		if time.Since(sess.Device.LastSeen) < time.Minute {
			s.setDeviceCookie(w, sess.Token)
		}
		h(w, r, sess)
	}
}

// ---- rendering ----

// page is the data handed to every template.
type page struct {
	Lang     string
	Path     string
	TitleKey string
	Title    string
	User     *store.User
	CSRF     string
	Flash    string
	FlashErr bool
	Data     any
	Theme    string // auto, light or dark
	HasBg    bool
	ThemeV   string // cache-busting version of /me/theme.css
}

func (s *Server) lang(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil && i18n.Supported(c.Value) {
		return c.Value
	}
	return i18n.Match(r.Header.Get("Accept-Language"), s.cfg.DefaultLang)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, sess *session, name string, status int, p *page) {
	p.Lang = s.lang(r)
	p.Path = r.URL.Path
	if p.TitleKey != "" {
		p.Title = i18n.T(p.Lang, p.TitleKey)
	}
	p.Theme = store.ThemeAuto
	if sess != nil {
		p.User = sess.User
		p.CSRF = s.auth.CSRF(sess.Device)
		if pr, err := s.st.GetPrefs(r.Context(), sess.User.ID); err == nil {
			p.Theme, p.HasBg, p.ThemeV = pr.Theme, pr.ShowsBackground() && backgroundURL(pr) != "", themeVersion(pr)
		}
	}
	if p.Flash == "" { // one-shot messages travel as ?ok=key / ?err=key
		if k := r.URL.Query().Get("ok"); strings.HasPrefix(k, "msg.") && i18n.Has(k) {
			p.Flash = k
		} else if k := r.URL.Query().Get("err"); strings.HasPrefix(k, "err.") && i18n.Has(k) {
			p.Flash, p.FlashErr = k, true
		}
	}
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		s.log.Error("render", "page", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

type errorData struct {
	Status     int
	MessageKey string
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, sess *session, status int, key string) {
	s.render(w, r, sess, "error", status, &page{TitleKey: key, Data: errorData{Status: status, MessageKey: key}})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, sess *session, err error) {
	s.log.Error("handler", "method", r.Method, "path", r.URL.Path, "err", err)
	s.renderError(w, r, sess, http.StatusInternalServerError, "err.server")
}

func formatTime(lang string, t time.Time) string {
	if i18n.Normalize(lang) == i18n.EN {
		return t.Local().Format("2006-01-02 15:04")
	}
	return t.Local().Format("02/01/2006 15:04")
}

// ---- cookies ----

func (s *Server) setDeviceCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: deviceCookie, Value: token, Path: "/",
		MaxAge:   int(s.auth.Config().DeviceTTL / time.Second),
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: deviceCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

// background runs f with a timeout that outlives the request.
func (s *Server) background(f func(ctx context.Context)) {
	s.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		f(ctx)
	})
}
