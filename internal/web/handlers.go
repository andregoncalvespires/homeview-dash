package web

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/andregoncalvespires/homeview-dash/internal/auth"
	"github.com/andregoncalvespires/homeview-dash/internal/catalog"
	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// ---- sign-in ----

type emailData struct{ Email string }

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, nil, "login", http.StatusOK, &page{TitleKey: "login.title", Data: emailData{}})
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, nil, http.StatusBadRequest, "err.badrequest")
		return
	}
	email := store.NormalizeEmail(r.PostFormValue("email"))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		s.render(w, r, nil, "login", http.StatusBadRequest, &page{
			TitleKey: "login.title", Flash: "err.email", FlashErr: true, Data: emailData{Email: email},
		})
		return
	}
	// The answer is identical for known and unknown addresses, and the e-mail
	// is sent in the background so response timing does not reveal anything.
	lang := s.lang(r)
	s.background(func(ctx context.Context) {
		if err := s.auth.RequestCode(ctx, email, lang); err != nil {
			if errors.Is(err, auth.ErrRateLimited) {
				s.log.Warn("login code rate limited")
			} else {
				s.log.Error("could not send login code", "err", err)
			}
		}
	})
	http.Redirect(w, r, "/verify?email="+url.QueryEscape(email), http.StatusSeeOther)
}

func (s *Server) getVerify(w http.ResponseWriter, r *http.Request) {
	email := store.NormalizeEmail(r.URL.Query().Get("email"))
	if email == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, nil, "verify", http.StatusOK, &page{TitleKey: "verify.title", Data: emailData{Email: email}})
}

func (s *Server) postVerify(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, nil, http.StatusBadRequest, "err.badrequest")
		return
	}
	email := store.NormalizeEmail(r.PostFormValue("email"))
	token, _, err := s.auth.VerifyCode(r.Context(), email, r.PostFormValue("code"), deviceLabel(r.UserAgent()))
	if errors.Is(err, auth.ErrInvalidCode) {
		s.render(w, r, nil, "verify", http.StatusUnauthorized, &page{
			TitleKey: "verify.title", Flash: "err.code", FlashErr: true, Data: emailData{Email: email},
		})
		return
	}
	if err != nil {
		s.serverError(w, r, nil, err)
		return
	}
	s.setDeviceCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// getLink redeems a one-time link created with `homeview admin login-link`.
func (s *Server) getLink(w http.ResponseWriter, r *http.Request) {
	token, _, err := s.auth.RedeemLinkToken(r.Context(), r.URL.Query().Get("token"), deviceLabel(r.UserAgent()))
	if errors.Is(err, auth.ErrInvalidCode) {
		s.renderError(w, r, nil, http.StatusBadRequest, "err.link")
		return
	}
	if err != nil {
		s.serverError(w, r, nil, err)
		return
	}
	s.setDeviceCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := s.st.DeleteDevice(r.Context(), sess.Device.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	s.clearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// getLang stores the language choice and returns to a local path.
func (s *Server) getLang(w http.ResponseWriter, r *http.Request) {
	if l := r.URL.Query().Get("set"); i18n.Supported(l) {
		http.SetCookie(w, &http.Cookie{
			Name: langCookie, Value: l, Path: "/", MaxAge: 365 * 24 * 3600,
			Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}

// safeNext only allows local absolute paths (no open redirect).
func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.Contains(next, "\\") &&
		!strings.ContainsAny(next, "\r\n") {
		return next
	}
	return "/"
}

// ---- dashboard ----

type serviceView struct {
	ID                int64
	Name, Description string
	Primary           string
	Secondary         string
	IconURL           string
	IconText          string
	Search            string
	Status            string // running/stopped for services that came from discovery
}

type groupView struct {
	ID       int64 // 0 for the "other" section
	Name     string
	Empty    bool // no services: shown to admins only, as a place to drop cards
	Services []serviceView
}

type dashboardData struct {
	Groups      []groupView
	HasServices bool
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, sess *session) {
	groups, err := s.st.ListGroups(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	services, err := s.st.ListServices(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	guests, err := s.st.ListGuests(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	guestStatus := map[int64]string{} // guest id -> last known status
	for _, g := range guests {
		if !g.Missing && (g.Status == "running" || g.Status == "stopped") {
			guestStatus[g.ID] = g.Status
		}
	}
	lang := s.lang(r)

	byGroup := map[int64]*groupView{}
	var ordered []*groupView
	for _, g := range groups {
		gv := &groupView{ID: g.ID, Name: g.Name}
		byGroup[g.ID] = gv
		ordered = append(ordered, gv)
	}
	other := &groupView{Name: i18n.T(lang, "dash.ungrouped")}

	for _, sv := range services {
		l := ChooseLinks(sv)
		if l.Primary == "" {
			continue
		}
		view := serviceView{
			ID: sv.ID, Name: sv.Name, Description: sv.Description,
			Primary: l.Primary, Secondary: l.Secondary,
			Status: guestStatus[sv.GuestID],
		}
		low := strings.ToLower(sv.Icon)
		if app, ok := catalog.FromIcon(sv.Icon); ok {
			view.IconURL = app.IconURL()
		} else if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
			view.IconURL = sv.Icon
		} else if !strings.HasPrefix(sv.Icon, catalog.IconPrefix) { // a dangling "app:" ref shows the default icon
			view.IconText = sv.Icon
		}
		target := other
		if g, ok := byGroup[sv.GroupID]; ok {
			target = g
		}
		view.Search = strings.ToLower(sv.Name + " " + sv.Description + " " + target.Name)
		target.Services = append(target.Services, view)
	}

	data := dashboardData{}
	for _, g := range append(ordered, other) {
		switch {
		case len(g.Services) > 0:
			data.Groups = append(data.Groups, *g)
			data.HasServices = true
		case sess.User.IsAdmin() && g.ID != 0: // empty groups are drop targets while rearranging
			g.Empty = true
			data.Groups = append(data.Groups, *g)
		}
	}
	s.render(w, r, sess, "dashboard", http.StatusOK, &page{Data: data})
}

// ---- devices ----

type deviceView struct {
	store.Device
	Current bool
}

type devicesData struct{ Devices []deviceView }

func (s *Server) getDevices(w http.ResponseWriter, r *http.Request, sess *session) {
	list, err := s.st.ListDevices(r.Context(), sess.User.ID)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	data := devicesData{}
	for _, d := range list {
		data.Devices = append(data.Devices, deviceView{Device: d, Current: d.ID == sess.Device.ID})
	}
	s.render(w, r, sess, "devices", http.StatusOK, &page{TitleKey: "devices.title", Data: data})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok || id == sess.Device.ID { // revoking the current device is "sign out"
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	// Only the owner's own devices can be revoked here.
	list, err := s.st.ListDevices(r.Context(), sess.User.ID)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	for _, d := range list {
		if d.ID == id {
			if err := s.st.DeleteDevice(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
				s.serverError(w, r, sess, err)
				return
			}
			break
		}
	}
	http.Redirect(w, r, "/devices?ok=msg.revoked", http.StatusSeeOther)
}
