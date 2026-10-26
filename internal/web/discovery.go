package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/catalog"
	"github.com/andregoncalvespires/homeview-dash/internal/discovery"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// SetDiscovery enables the Proxmox connection and discovery screens.
func (s *Server) SetDiscovery(d *discovery.Service) { s.disc = d }

// extendWriteDeadline gives slow Proxmox calls more time than the server-wide
// write timeout allows. Errors are ignored: not every ResponseWriter supports it.
func extendWriteDeadline(w http.ResponseWriter, d time.Duration) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
}

// ---- connections ----

// connForm carries the connection form values (never the token secret).
type connForm struct {
	ID          int64
	Name        string
	URL         string
	TokenID     string
	Fingerprint string
	Insecure    bool
}

type connRow struct {
	store.Connection
	Guests, New int
}

type connectionsData struct {
	Edit   connForm
	Rows   []connRow
	Detail string // technical reason of a failed connection test
}

func (s *Server) loadConnections(r *http.Request, edit connForm, detail string) (connectionsData, error) {
	conns, err := s.st.ListConnections(r.Context())
	if err != nil {
		return connectionsData{}, err
	}
	guests, err := s.st.ListGuests(r.Context())
	if err != nil {
		return connectionsData{}, err
	}
	data := connectionsData{Edit: edit, Detail: detail}
	for _, c := range conns {
		row := connRow{Connection: c}
		for _, g := range guests {
			if g.ConnID == c.ID {
				row.Guests++
				if g.State == store.GuestNew && !g.Missing {
					row.New++
				}
			}
		}
		data.Rows = append(data.Rows, row)
	}
	return data, nil
}

func (s *Server) getAdminConnections(w http.ResponseWriter, r *http.Request, sess *session) {
	var edit connForm
	if id, err := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64); err == nil {
		if c, err := s.st.GetConnection(r.Context(), id); err == nil {
			edit = connForm{ID: c.ID, Name: c.Name, URL: c.URL, TokenID: c.TokenID, Fingerprint: c.Fingerprint, Insecure: c.Insecure}
		}
	}
	data, err := s.loadConnections(r, edit, "")
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_connections", http.StatusOK, &page{TitleKey: "admin.conn.title", Data: data})
}

func (s *Server) saveConnection(w http.ResponseWriter, r *http.Request, sess *session) {
	if s.disc == nil {
		s.serverError(w, r, sess, errors.New("discovery is not configured"))
		return
	}
	var id int64
	if v, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64); err == nil && v > 0 {
		id = v
	}
	in := discovery.ConnInput{
		Name: r.PostFormValue("name"), URL: r.PostFormValue("url"), TokenID: r.PostFormValue("token_id"),
		Secret: r.PostFormValue("secret"), Fingerprint: r.PostFormValue("fingerprint"),
		Insecure: r.PostFormValue("insecure") != "",
	}
	extendWriteDeadline(w, 60*time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	_, err := s.disc.SaveConnection(ctx, id, in)
	if err == nil {
		http.Redirect(w, r, "/admin/discovery?ok=msg.conn_saved", http.StatusSeeOther)
		return
	}

	var (
		key    string
		detail string
		status = http.StatusBadRequest
		ie     *discovery.InputError
		ce     *discovery.ConnectError
	)
	switch {
	case errors.As(err, &ie):
		key = ie.Key
	case errors.As(err, &ce):
		key, detail = "err.conn_failed", ce.Error()
	case errors.Is(err, store.ErrExists):
		key = "err.conn_exists"
	case errors.Is(err, store.ErrNotFound):
		s.renderError(w, r, sess, http.StatusNotFound, "err.notfound")
		return
	default:
		s.serverError(w, r, sess, err)
		return
	}
	form := connForm{ID: id, Name: in.Name, URL: in.URL, TokenID: in.TokenID, Fingerprint: in.Fingerprint, Insecure: in.Insecure}
	data, lerr := s.loadConnections(r, form, detail)
	if lerr != nil {
		s.serverError(w, r, sess, lerr)
		return
	}
	s.render(w, r, sess, "admin_connections", status, &page{
		TitleKey: "admin.conn.title", Flash: key, FlashErr: true, Data: data,
	})
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	if err := s.st.DeleteConnection(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/connections?ok=msg.deleted", http.StatusSeeOther)
}

// ---- discovered guests ----

// approveForm is the "add to dashboard" form of one guest.
type approveForm struct {
	GuestID  int64
	Service  store.Service
	GroupID  int64        // group picked from the list (0 = none)
	NewGroup string       // name typed to create/reuse a group; wins over GroupID
	Open     bool         // render the <details> expanded (after a validation error)
	App      *catalog.App // catalog entry recognised from the guest name, if any
}

type guestRow struct {
	store.Guest
	Form     approveForm
	Services []store.Service // what already runs on this machine
	Apps     []catalog.App   // for the application picker
}

type connSection struct {
	Conn     store.Connection
	New      []guestRow
	Ignored  []guestRow
	Approved []guestRow
	Missing  []guestRow // gone from Proxmox and not approved
}

type discoveryData struct {
	Sections   []connSection
	Groups     []store.Group // suggestions for the group field
	Connection bool          // at least one connection exists
}

// defaultForm pre-fills the "add to dashboard" form of a guest. The group
// defaults to the existing one named like the node, or proposes creating it.
func defaultForm(g store.Guest, groups []store.Group) approveForm {
	f := approveForm{GuestID: g.ID}
	for _, gr := range groups {
		if strings.EqualFold(gr.Name, g.Node) {
			f.GroupID = gr.ID
		}
	}
	if f.GroupID == 0 {
		f.NewGroup = g.Node
	}
	f.Service.Name = g.Name
	if g.Kind == "lxc" {
		f.Service.Icon = "📦"
	} else {
		f.Service.Icon = "🖥️"
	}
	if app, ok := catalog.Match(g.Name); ok {
		f.App = &app
		f.Service.Icon = app.IconRef()
		f.Service.InternalURL = app.SuggestedURL(g.IP)
	}
	if f.Service.InternalURL == "" && g.IP != "" {
		f.Service.InternalURL = "http://" + g.IP
	}
	return f
}

func (s *Server) loadDiscovery(r *http.Request, override *approveForm) (discoveryData, error) {
	ctx := r.Context()
	conns, err := s.st.ListConnections(ctx)
	if err != nil {
		return discoveryData{}, err
	}
	guests, err := s.st.ListGuests(ctx)
	if err != nil {
		return discoveryData{}, err
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		return discoveryData{}, err
	}
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		return discoveryData{}, err
	}
	byGuest := map[int64][]store.Service{}
	for _, sv := range services {
		if sv.GuestID != 0 {
			byGuest[sv.GuestID] = append(byGuest[sv.GuestID], sv)
		}
	}
	apps := catalog.All()
	data := discoveryData{Groups: groups, Connection: len(conns) > 0}
	for _, c := range conns {
		sec := connSection{Conn: c}
		for _, g := range guests {
			if g.ConnID != c.ID {
				continue
			}
			row := guestRow{Guest: g, Form: defaultForm(g, groups), Services: byGuest[g.ID], Apps: apps}
			if len(row.Services) > 0 { // one more service on a machine that already has some: start blank
				app := (*catalog.App)(nil)
				row.Form.Service.Name, row.Form.Service.Icon, row.Form.App = "", "", app
				row.Form.Service.InternalURL = ""
				if g.IP != "" {
					row.Form.Service.InternalURL = "http://" + g.IP
				}
				row.Form.GroupID = row.Services[0].GroupID
				row.Form.NewGroup = ""
			}
			if override != nil && override.GuestID == g.ID {
				app := row.Form.App
				row.Form = *override
				row.Form.App = app
			}
			switch {
			case g.State == store.GuestApproved:
				sec.Approved = append(sec.Approved, row)
			case g.Missing:
				sec.Missing = append(sec.Missing, row)
			case g.State == store.GuestIgnored:
				sec.Ignored = append(sec.Ignored, row)
			default:
				sec.New = append(sec.New, row)
			}
		}
		data.Sections = append(data.Sections, sec)
	}
	return data, nil
}

func (s *Server) getAdminDiscovery(w http.ResponseWriter, r *http.Request, sess *session) {
	data, err := s.loadDiscovery(r, nil)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_discovery", http.StatusOK, &page{TitleKey: "admin.disc.title", Data: data})
}

// syncDiscovery refreshes one connection (id given) or all of them.
func (s *Server) syncDiscovery(w http.ResponseWriter, r *http.Request, sess *session) {
	if s.disc == nil {
		s.serverError(w, r, sess, errors.New("discovery is not configured"))
		return
	}
	var ids []int64
	if id, ok := formID(r); ok {
		ids = append(ids, id)
	} else {
		conns, err := s.st.ListConnections(r.Context())
		if err != nil {
			s.serverError(w, r, sess, err)
			return
		}
		for _, c := range conns {
			ids = append(ids, c.ID)
		}
	}
	extendWriteDeadline(w, 110*time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	failed := false
	for _, id := range ids {
		if _, err := s.disc.Sync(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
			failed = true // the reason is stored on the connection and shown on the page
			s.log.Warn("manual discovery sync failed", "connection", id, "err", err)
		}
	}
	if failed {
		http.Redirect(w, r, "/admin/discovery?err=err.sync_failed", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/discovery?ok=msg.synced", http.StatusSeeOther)
}

// groupFor returns the id of the group called name (case-insensitive),
// creating it when needed. An empty name means "no group".
func (s *Server) groupFor(ctx context.Context, name string) (int64, error) {
	if name == "" {
		return 0, nil
	}
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		return 0, err
	}
	pos := 0
	for _, g := range groups {
		if strings.EqualFold(g.Name, name) {
			return g.ID, nil
		}
		if g.Position >= pos {
			pos = g.Position + 1
		}
	}
	g := store.Group{Name: name, Position: pos}
	if err := s.st.SaveGroup(ctx, &g); err != nil {
		return 0, err
	}
	return g.ID, nil
}

func (s *Server) approveGuest(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	ctx := r.Context()
	g, err := s.st.GetGuest(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/admin/discovery?err=err.guest_state", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	// Any number of services may be added to one machine, so an approved guest
	// is fine here. A missing one is not: there is nothing left to point at.
	if g.Missing {
		http.Redirect(w, r, "/admin/discovery?err=err.guest_state", http.StatusSeeOther)
		return
	}

	form := approveForm{GuestID: id, Open: true, Service: store.Service{GuestID: id,
		Name: r.PostFormValue("name"), Description: r.PostFormValue("description"), Icon: r.PostFormValue("icon"),
		InternalURL: r.PostFormValue("internal_url"), ExternalURL: r.PostFormValue("external_url"),
	}}
	if gid, err := strconv.ParseInt(r.PostFormValue("group_id"), 10, 64); err == nil && gid > 0 {
		form.GroupID = gid
	}
	var groupOK bool
	form.NewGroup, groupOK = store.CleanText(r.PostFormValue("new_group"), 80)
	uploaded, upKey, upErr := s.iconFromUpload(r)
	if upErr != nil {
		s.serverError(w, r, sess, upErr)
		return
	}
	saved := false
	defer func() {
		if !saved && uploaded != "" {
			s.pruneIcon(context.WithoutCancel(ctx), uploaded)
		}
	}()
	if uploaded != "" {
		form.Service.Icon = uploaded
	}
	key := upKey
	if key == "" {
		key = validateService(&form.Service)
	}
	if key == "" && strings.HasPrefix(form.Service.Icon, uploadIconPrefix) && !s.iconExists(ctx, form.Service.Icon) {
		key = "err.icon_gone"
	}
	if key == "" && !groupOK {
		key = "err.name"
	}
	if key != "" {
		s.rerenderDiscovery(w, r, sess, &form, key)
		return
	}

	form.Service.GroupID = form.GroupID
	if form.NewGroup != "" { // typing a name wins over the list; it is created when new
		gid, err := s.groupFor(ctx, form.NewGroup)
		if err != nil {
			s.serverError(w, r, sess, err)
			return
		}
		form.Service.GroupID = gid
	}
	if err := s.st.SaveService(ctx, &form.Service); err != nil {
		if errors.Is(err, store.ErrNotFound) { // the machine was removed meanwhile
			http.Redirect(w, r, "/admin/discovery?err=err.guest_state", http.StatusSeeOther)
			return
		}
		s.serverError(w, r, sess, err)
		return
	}
	saved = true
	if g.State == store.GuestIgnored { // adding a service means it is wanted
		if err := s.st.SetGuestState(ctx, id, store.GuestNew); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.serverError(w, r, sess, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/discovery?ok=msg.guest_approved", http.StatusSeeOther)
}

func (s *Server) rerenderDiscovery(w http.ResponseWriter, r *http.Request, sess *session, f *approveForm, key string) {
	data, err := s.loadDiscovery(r, f)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_discovery", http.StatusBadRequest, &page{
		TitleKey: "admin.disc.title", Flash: key, FlashErr: true, Data: data,
	})
}

// setGuestState implements ignore / restore.
func (s *Server) setGuestState(state store.GuestState, okKey string) func(http.ResponseWriter, *http.Request, *session) {
	return func(w http.ResponseWriter, r *http.Request, sess *session) {
		id, ok := formID(r)
		if !ok {
			s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
			return
		}
		g, err := s.st.GetGuest(r.Context(), id)
		if err == nil && g.State == store.GuestApproved {
			http.Redirect(w, r, "/admin/discovery?err=err.guest_state", http.StatusSeeOther)
			return
		}
		if err == nil {
			err = s.st.SetGuestState(r.Context(), id, state)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.serverError(w, r, sess, err)
			return
		}
		http.Redirect(w, r, "/admin/discovery?ok="+okKey, http.StatusSeeOther)
	}
}

// forgetGuest removes a guest that no longer exists in Proxmox.
func (s *Server) forgetGuest(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	g, err := s.st.GetGuest(r.Context(), id)
	if err == nil && (!g.Missing || g.State == store.GuestApproved) {
		http.Redirect(w, r, "/admin/discovery?err=err.guest_state", http.StatusSeeOther)
		return
	}
	if err == nil {
		err = s.st.DeleteGuest(r.Context(), id)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/discovery?ok=msg.deleted", http.StatusSeeOther)
}
