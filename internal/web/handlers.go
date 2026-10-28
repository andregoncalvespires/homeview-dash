package web

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
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
	ID       int64  // group id (0 for the "other" section); only meaningful in the groups view
	Key      string // stable key of the section, used to remember collapsed sections
	Name     string
	Sub      string // small detail next to the name (a machine's address, say)
	Status   string // running/stopped for a machine section
	Empty    bool   // no services: shown to admins only, as a place to drop cards
	Services []serviceView
}

type dashboardData struct {
	Groups      []groupView
	HasServices bool
	Mode        string // viewGroups, viewInfra or viewType
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, sess *session) {
	if v := r.URL.Query().Get("view"); v != "" {
		if validView(v) {
			http.SetCookie(w, &http.Cookie{
				Name: viewCookie, Value: v, Path: "/", MaxAge: 365 * 24 * 3600,
				HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
			})
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	mode := viewGroups
	if c, err := r.Cookie(viewCookie); err == nil && validView(c.Value) {
		mode = c.Value
	}
	ctx := r.Context()
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	guests, err := s.st.ListGuests(ctx)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	visible := visibleGroups(groups, sess.User)
	guestByID := map[int64]store.Guest{}
	for _, g := range guests {
		guestByID[g.ID] = g
	}
	lang := s.lang(r)

	// A card for every service that has somewhere to go.
	type item struct {
		sv   store.Service
		view serviceView
	}
	var items []item
	for _, sv := range services {
		l := ChooseLinks(sv)
		if l.Primary == "" || (sv.GroupID != 0 && !visible[sv.GroupID]) {
			continue
		}
		v := serviceView{ID: sv.ID, Name: sv.Name, Description: sv.Description, Primary: l.Primary, Secondary: l.Secondary}
		if g, ok := guestByID[sv.GuestID]; ok && !g.Missing && (g.Status == "running" || g.Status == "stopped") {
			v.Status = g.Status
		}
		v.IconURL, v.IconText = resolveIcon(sv.Icon)
		items = append(items, item{sv, v})
	}
	finish := func(sections []*groupView) dashboardData {
		d := dashboardData{Mode: mode}
		for _, g := range sections {
			switch {
			case len(g.Services) > 0:
				d.Groups = append(d.Groups, *g)
				d.HasServices = true
			case mode == viewGroups && sess.User.IsAdmin() && g.ID != 0: // empty groups are drop targets while rearranging
				g.Empty = true
				d.Groups = append(d.Groups, *g)
			}
		}
		return d
	}
	add := func(g *groupView, it item) {
		it.view.Search = strings.ToLower(it.sv.Name + " " + it.sv.Description + " " + g.Name + " " + g.Sub)
		g.Services = append(g.Services, it.view)
	}

	var sections []*groupView
	switch mode {
	case viewInfra:
		// Machines ordered by Proxmox node, then kind and id; whatever is not tied to a machine goes last.
		sort.SliceStable(guests, func(i, j int) bool {
			a, b := guests[i], guests[j]
			if a.Node != b.Node {
				return a.Node < b.Node
			}
			return a.VMID < b.VMID
		})
		byGuest := map[int64]*groupView{}
		nodeSec := map[string]*groupView{}
		nodeAddr := map[string]string{}
		if nodes, err := s.st.ListNodes(ctx); err == nil {
			for _, n := range nodes {
				nodeAddr[n.Name] = n.Addr
			}
		}
		for _, it := range items { // host services: one section per Proxmox node
			if it.sv.GuestID == 0 && it.sv.Node != "" && nodeSec[it.sv.Node] == nil {
				nodeSec[it.sv.Node] = &groupView{Key: "n" + it.sv.Node, Name: "Proxmox " + it.sv.Node, Sub: nodeAddr[it.sv.Node]}
			}
		}
		nodeDone := map[string]bool{}
		for _, g := range guests {
			if ns := nodeSec[g.Node]; ns != nil && !nodeDone[g.Node] { // a node comes before its machines
				sections = append(sections, ns)
				nodeDone[g.Node] = true
			}
			kind := "LXC"
			if g.Kind == "qemu" {
				kind = "VM"
			}
			sub := strings.TrimSpace(g.Node + " · " + kind + " " + strconv.Itoa(g.VMID))
			if g.IP != "" {
				sub += " · " + g.IP
			}
			gv := &groupView{Key: "i" + strconv.FormatInt(g.ID, 10), Name: g.Name, Sub: sub}
			if !g.Missing && (g.Status == "running" || g.Status == "stopped") {
				gv.Status = g.Status
			}
			byGuest[g.ID] = gv
			sections = append(sections, gv)
		}
		var extra []string // nodes without any discovered machine
		for name := range nodeSec {
			if !nodeDone[name] {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			sections = append(sections, nodeSec[name])
		}
		loose := &groupView{Key: "i0", Name: i18n.T(lang, "dash.nomachine")}
		for _, it := range items {
			if gv, ok := byGuest[it.sv.GuestID]; ok && it.sv.GuestID != 0 {
				add(gv, it)
			} else if ns := nodeSec[it.sv.Node]; ns != nil && it.sv.GuestID == 0 {
				add(ns, it)
			} else {
				add(loose, it)
			}
		}
		sections = append(sections, loose)
	case viewType:
		by := map[string]*groupView{}
		for _, c := range catalog.Categories {
			gv := &groupView{Key: "t" + c, Name: i18n.T(lang, "cat."+c)}
			by[c] = gv
			sections = append(sections, gv)
		}
		for _, it := range items {
			c := catalog.CatOther
			if it.sv.Type != "" && by[it.sv.Type] != nil {
				c = it.sv.Type
			} else if app, ok := catalog.FromIcon(it.sv.Icon); ok {
				c = catalog.Category(app.ID)
			}
			add(by[c], it)
		}
	default:
		byGroup := map[int64]*groupView{}
		for _, g := range withPaths(groups) {
			if !visible[g.ID] {
				continue
			}
			gv := &groupView{ID: g.ID, Key: strconv.FormatInt(g.ID, 10), Name: g.Name}
			byGroup[g.ID] = gv
			sections = append(sections, gv)
		}
		other := &groupView{Key: "0", Name: i18n.T(lang, "dash.ungrouped")}
		for _, it := range items {
			if g, ok := byGroup[it.sv.GroupID]; ok {
				add(g, it)
			} else {
				add(other, it)
			}
		}
		sections = append(sections, other)
	}
	data := finish(sections)
	s.render(w, r, sess, "dashboard", http.StatusOK, &page{Data: data, Views: s.viewMenu(lang, mode), Search: data.HasServices})
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

// resolveIcon turns a stored icon value into an image URL or a short text
// (an emoji). Both are empty for a dangling reference, which shows the default.
func resolveIcon(icon string) (url, text string) {
	low := strings.ToLower(icon)
	switch {
	case icon == "":
		return "", ""
	case strings.HasPrefix(icon, catalog.IconPrefix):
		if app, ok := catalog.FromIcon(icon); ok {
			return app.IconURL(), ""
		}
		return "", ""
	case strings.HasPrefix(icon, uploadIconPrefix):
		if id, ok := uploadIconID(icon); ok {
			return "/icons/" + strconv.FormatInt(id, 10), ""
		}
		return "", ""
	case strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://"):
		return icon, ""
	}
	return "", icon
}
