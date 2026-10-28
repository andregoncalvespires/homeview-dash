package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"

	"github.com/andregoncalvespires/homeview-dash/internal/catalog"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// formID reads a positive integer "id" from the submitted form.
func formID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// formInt reads a bounded integer, defaulting to 0 when empty or invalid.
func formInt(r *http.Request, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue(name)))
	if err != nil || n < -9999 || n > 9999 {
		return 0
	}
	return n
}

// ---- services ----

type serviceRow struct {
	store.Service
	GroupName string
}

type adminServicesData struct {
	Edit     store.Service
	Groups   []store.Group
	Rows     []serviceRow
	Apps     []catalog.App // catalog entries for the "application" picker
	NewGroup string        // text typed in "or create a new group", kept across a failed save
	Origins  []originOpt   // machines and Proxmox hosts a service can run on
	IconURL  string        // preview of the icon being edited, if it is an image
	Cats     []string      // service categories for the "type" picker
}

// originOpt is one entry of the "where does it run" picker.
type originOpt struct {
	Value    string // "g:<guest id>" links the service to a machine; "h:<addr>" only fills the address
	Label    string
	Addr     string
	Host     bool // a Proxmox host rather than a VM/container
	Selected bool
}

func (s *Server) loadServicesPage(r *http.Request, edit store.Service) (adminServicesData, error) {
	groups, err := s.st.ListGroups(r.Context())
	if err != nil {
		return adminServicesData{}, err
	}
	services, err := s.st.ListServices(r.Context())
	if err != nil {
		return adminServicesData{}, err
	}
	groups = withPaths(groups)
	names := map[int64]string{}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	data := adminServicesData{Edit: edit, Groups: groups, Apps: catalog.All(), Cats: catalog.Categories}
	data.IconURL, _ = resolveIcon(edit.Icon)
	if data.Origins, err = s.origins(r.Context(), edit.GuestID, edit.Node); err != nil {
		return adminServicesData{}, err
	}
	for _, sv := range services {
		data.Rows = append(data.Rows, serviceRow{Service: sv, GroupName: names[sv.GroupID]})
	}
	sort.SliceStable(data.Rows, func(i, j int) bool { return sortKey(data.Rows[i].Name) < sortKey(data.Rows[j].Name) })
	sort.SliceStable(data.Groups, func(i, j int) bool { return sortKey(data.Groups[i].Name) < sortKey(data.Groups[j].Name) })
	return data, nil
}

func (s *Server) getAdminServices(w http.ResponseWriter, r *http.Request, sess *session) {
	var edit store.Service
	if raw := r.URL.Query().Get("edit"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			if sv, err := s.st.GetService(r.Context(), id); err == nil {
				edit = *sv
			}
		}
	}
	data, err := s.loadServicesPage(r, edit)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_services", http.StatusOK, &page{TitleKey: "admin.services.title", Data: data})
}

// validateService normalises fields and returns an i18n error key ("" = valid).
func validateService(v *store.Service) string {
	var ok bool
	if v.Name, ok = store.CleanText(v.Name, 80); !ok || v.Name == "" {
		return "err.name"
	}
	if v.Description, ok = store.CleanText(v.Description, 200); !ok {
		return "err.description"
	}
	v.Icon = strings.TrimSpace(v.Icon)
	v.InternalURL = strings.TrimSpace(v.InternalURL)
	v.ExternalURL = strings.TrimSpace(v.ExternalURL)
	switch {
	case v.InternalURL == "" && v.ExternalURL == "":
		return "err.url_required"
	case v.InternalURL != "" && !validHTTPURL(v.InternalURL),
		v.ExternalURL != "" && !validHTTPURL(v.ExternalURL):
		return "err.url"
	case !validIcon(v.Icon):
		return "err.icon"
	case !validType(v.Type):
		return "err.type"
	}
	return ""
}

func (s *Server) saveService(w http.ResponseWriter, r *http.Request, sess *session) {
	v := store.Service{
		Name:        r.PostFormValue("name"),
		Description: r.PostFormValue("description"),
		Icon:        r.PostFormValue("icon"),
		InternalURL: r.PostFormValue("internal_url"),
		ExternalURL: r.PostFormValue("external_url"),
		Position:    formInt(r, "position"),
	}
	if id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64); err == nil && id > 0 {
		v.ID = id
	}
	if gid, err := strconv.ParseInt(r.PostFormValue("group_id"), 10, 64); err == nil && gid > 0 {
		v.GroupID = gid
	}
	v.Type = strings.TrimSpace(r.PostFormValue("type"))
	origin := r.PostFormValue("origin")
	v.GuestID = guestFromOrigin(origin)
	if v.GuestID == 0 {
		v.Node = s.nodeFromOrigin(r.Context(), origin)
	}
	newGroup, groupOK := store.CleanText(r.PostFormValue("new_group"), 80)

	// An image sent from the computer replaces whatever the icon field says.
	ctx := r.Context()
	var oldIcon string
	if v.ID > 0 {
		if cur, err := s.st.GetService(ctx, v.ID); err == nil {
			oldIcon = cur.Icon
		}
	}
	uploaded, upKey, upErr := s.iconFromUpload(r)
	if upErr != nil {
		s.serverError(w, r, sess, upErr)
		return
	}
	saved := false
	defer func() {
		if !saved && uploaded != "" {
			s.pruneIcon(context.WithoutCancel(ctx), uploaded) // the form failed: do not keep the image
		}
	}()
	if upKey != "" {
		s.rerenderServiceForm(w, r, sess, v, newGroup, upKey)
		return
	}
	if uploaded != "" {
		v.Icon = uploaded
	}
	if key := validateService(&v); key != "" {
		s.rerenderServiceForm(w, r, sess, v, newGroup, key)
		return
	} else if strings.HasPrefix(v.Icon, uploadIconPrefix) && !s.iconExists(ctx, v.Icon) {
		s.rerenderServiceForm(w, r, sess, v, newGroup, "err.icon_gone")
		return
	} else if !groupOK {
		s.rerenderServiceForm(w, r, sess, v, newGroup, "err.name")
		return
	}
	if newGroup != "" { // typing a name wins over the list; the group is created when new
		gid, err := s.groupFor(r.Context(), newGroup)
		if err != nil {
			s.serverError(w, r, sess, err)
			return
		}
		v.GroupID = gid
	}
	if err := s.st.SaveService(r.Context(), &v); err != nil {
		if errors.Is(err, store.ErrNotFound) { // group, machine or the service itself vanished
			s.rerenderServiceForm(w, r, sess, v, newGroup, "err.group_missing")
			return
		}
		s.serverError(w, r, sess, err)
		return
	}
	saved = true
	if oldIcon != "" && oldIcon != v.Icon {
		s.pruneIcon(ctx, oldIcon)
	}
	http.Redirect(w, r, "/admin/services?ok=msg.saved", http.StatusSeeOther)
}

func (s *Server) rerenderServiceForm(w http.ResponseWriter, r *http.Request, sess *session, v store.Service, newGroup, key string) {
	data, err := s.loadServicesPage(r, v)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	data.NewGroup = newGroup
	s.render(w, r, sess, "admin_services", http.StatusBadRequest, &page{
		TitleKey: "admin.services.title", Flash: key, FlashErr: true, Data: data,
	})
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	var icon string
	if cur, err := s.st.GetService(r.Context(), id); err == nil {
		icon = cur.Icon
	}
	if err := s.st.DeleteService(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	s.pruneIcon(r.Context(), icon)
	http.Redirect(w, r, "/admin/services?ok=msg.deleted", http.StatusSeeOther)
}

// ---- groups ----

type groupRow struct {
	store.Group
	ParentName string
	Services   int         // how many services are in it
	Parents    []parentOpt // possible parents (top-level groups other than itself), when it may have one
	Users      []userOpt
}

type parentOpt struct {
	ID       int64
	Name     string
	Selected bool
}

type userOpt struct {
	ID      int64
	Name    string
	Email   string
	Checked bool
}

type adminGroupsData struct {
	Rows []groupRow // the list, parents followed by their sub-groups, alphabetical
	Form *groupRow  // the group being edited or created; nil keeps the form closed
	New  bool
}

// sortKey orders names alphabetically ignoring case and Portuguese accents.
func sortKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'â', 'ã', 'ä':
			r = 'a'
		case 'é', 'è', 'ê', 'ë':
			r = 'e'
		case 'í', 'ì', 'î', 'ï':
			r = 'i'
		case 'ó', 'ò', 'ô', 'õ', 'ö':
			r = 'o'
		case 'ú', 'ù', 'û', 'ü':
			r = 'u'
		case 'ç':
			r = 'c'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *Server) loadGroupsPage(ctx context.Context, editID int64, isNew bool) (adminGroupsData, error) {
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		return adminGroupsData{}, err
	}
	users, err := s.st.ListUsers(ctx)
	if err != nil {
		return adminGroupsData{}, err
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		return adminGroupsData{}, err
	}
	count := map[int64]int{}
	for _, sv := range services {
		count[sv.GroupID]++
	}
	name := map[int64]string{}
	hasChild := map[int64]bool{}
	for _, g := range groups {
		name[g.ID] = g.Name
		if g.ParentID != 0 {
			hasChild[g.ParentID] = true
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return sortKey(groups[i].Name) < sortKey(groups[j].Name) })
	var parents []parentOpt
	for _, g := range groups {
		if g.ParentID == 0 {
			parents = append(parents, parentOpt{ID: g.ID, Name: g.Name})
		}
	}
	userOpts := func(g *store.Group) []userOpt {
		var out []userOpt
		for _, u := range users {
			if u.IsAdmin() { // administrators always see everything
				continue
			}
			o := userOpt{ID: u.ID, Name: u.Name, Email: u.Email}
			if g != nil {
				for _, id := range g.UserIDs {
					if id == u.ID {
						o.Checked = true
					}
				}
			}
			out = append(out, o)
		}
		return out
	}
	build := func(g store.Group) groupRow {
		row := groupRow{Group: g, ParentName: name[g.ParentID], Services: count[g.ID], Users: userOpts(&g)}
		if !hasChild[g.ID] { // a group with sub-groups stays at the top
			for _, p := range parents {
				if p.ID != g.ID {
					p.Selected = p.ID == g.ParentID
					row.Parents = append(row.Parents, p)
				}
			}
		}
		return row
	}
	data := adminGroupsData{}
	for _, g := range groups { // parents first, each followed by its sub-groups
		if g.ParentID != 0 && name[g.ParentID] != "" {
			continue
		}
		data.Rows = append(data.Rows, build(g))
		for _, ch := range groups {
			if ch.ParentID == g.ID {
				data.Rows = append(data.Rows, build(ch))
			}
		}
	}
	switch {
	case isNew:
		f := build(store.Group{})
		f.Parents = append([]parentOpt(nil), parents...)
		data.Form, data.New = &f, true
	case editID != 0:
		for i := range data.Rows {
			if data.Rows[i].ID == editID {
				data.Form = &data.Rows[i]
			}
		}
	}
	return data, nil
}

func (s *Server) getAdminGroups(w http.ResponseWriter, r *http.Request, sess *session) {
	editID, _ := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64)
	data, err := s.loadGroupsPage(r.Context(), editID, r.URL.Query().Get("new") != "")
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_groups", http.StatusOK, &page{TitleKey: "admin.groups.title", Data: data})
}

func (s *Server) saveGroup(w http.ResponseWriter, r *http.Request, sess *session) {
	ctx := r.Context()
	name, ok := store.CleanText(r.PostFormValue("name"), 80)
	g := store.Group{Name: name, Position: formInt(r, "position"), Restricted: r.PostFormValue("restricted") != ""}
	if !ok || g.Name == "" {
		http.Redirect(w, r, "/admin/groups?err=err.name", http.StatusSeeOther)
		return
	}
	if id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64); err == nil && id > 0 {
		g.ID = id
	}
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	byID := map[int64]store.Group{}
	for _, x := range groups {
		byID[x.ID] = x
	}
	if pid, err := strconv.ParseInt(r.PostFormValue("parent_id"), 10, 64); err == nil && pid > 0 {
		parent, exists := byID[pid]
		hasChild := false
		for _, x := range groups {
			if x.ParentID == g.ID && g.ID != 0 {
				hasChild = true
			}
		}
		if !exists || parent.ParentID != 0 || pid == g.ID || hasChild { // one level only
			http.Redirect(w, r, "/admin/groups?err=err.parent", http.StatusSeeOther)
			return
		}
		g.ParentID = pid
	}
	if g.Restricted {
		users, err := s.st.ListUsers(ctx)
		if err != nil {
			s.serverError(w, r, sess, err)
			return
		}
		ok := map[int64]bool{}
		for _, u := range users {
			ok[u.ID] = !u.IsAdmin()
		}
		for _, raw := range r.PostForm["users"] {
			if uid, err := strconv.ParseInt(raw, 10, 64); err == nil && ok[uid] {
				g.UserIDs = append(g.UserIDs, uid)
			}
		}
	}
	if err := s.st.SaveGroup(ctx, &g); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/groups?ok=msg.saved", http.StatusSeeOther)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	if err := s.st.DeleteGroup(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/groups?ok=msg.deleted", http.StatusSeeOther)
}

// ---- users ----

type adminUserRow struct {
	store.User
	PX store.MetricsAccess
}

type adminUsersData struct{ Users []adminUserRow }

func (s *Server) getAdminUsers(w http.ResponseWriter, r *http.Request, sess *session) {
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	access, err := s.st.ListMetricsAccess(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	byUser := map[int64]store.MetricsAccess{}
	for _, a := range access {
		byUser[a.UserID] = a
	}
	data := adminUsersData{}
	for _, u := range users {
		data.Users = append(data.Users, adminUserRow{User: u, PX: byUser[u.ID]})
	}
	s.render(w, r, sess, "admin_users", http.StatusOK, &page{TitleKey: "admin.users.title", Data: data})
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request, sess *session) {
	email := store.NormalizeEmail(r.PostFormValue("email"))
	name, nameOK := store.CleanText(r.PostFormValue("name"), 80)
	role := store.Role(r.PostFormValue("role"))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		http.Redirect(w, r, "/admin/users?err=err.email", http.StatusSeeOther)
		return
	}
	if !nameOK || name == "" || !role.Valid() {
		http.Redirect(w, r, "/admin/users?err=err.name", http.StatusSeeOther)
		return
	}
	if _, err := s.st.CreateUser(r.Context(), email, name, role); err != nil {
		if errors.Is(err, store.ErrExists) {
			http.Redirect(w, r, "/admin/users?err=err.user_exists", http.StatusSeeOther)
			return
		}
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/users?ok=msg.user_added", http.StatusSeeOther)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	if id == sess.User.ID {
		http.Redirect(w, r, "/admin/users?err=err.user_self", http.StatusSeeOther)
		return
	}
	target, err := s.st.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/admin/users?ok=msg.deleted", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	if target.IsAdmin() {
		if n, err := s.st.CountAdmins(r.Context()); err != nil {
			s.serverError(w, r, sess, err)
			return
		} else if n <= 1 {
			http.Redirect(w, r, "/admin/users?err=err.last_admin", http.StatusSeeOther)
			return
		}
	}
	if err := s.st.DeleteUser(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/users?ok=msg.deleted", http.StatusSeeOther)
}

// origins lists where a service can run: discovered machines (which also link
// the service to them) and the Proxmox hosts themselves (address only).
// keep is a guest that stays listed even when it is gone or ignored, so an
// existing service still shows where it belongs.
func (s *Server) origins(ctx context.Context, keep int64, keepNode string) ([]originOpt, error) {
	guests, err := s.st.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.st.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	var out []originOpt
	for _, g := range guests {
		if (g.Missing || g.State == store.GuestIgnored) && g.ID != keep {
			continue
		}
		kind := "VM"
		if g.Kind == "lxc" {
			kind = "LXC"
		}
		label := fmt.Sprintf("%s · %s %d", g.Name, kind, g.VMID)
		if g.IP != "" {
			label += " · " + g.IP
		}
		out = append(out, originOpt{Value: "g:" + strconv.FormatInt(g.ID, 10), Label: label, Addr: g.IP, Selected: g.ID == keep})
	}
	for _, n := range nodes {
		if n.Addr == "" {
			continue
		}
		out = append(out, originOpt{Value: "h:" + n.Addr, Label: "Proxmox " + n.Name + " · " + n.Addr, Addr: n.Addr, Host: true, Selected: keep == 0 && keepNode != "" && n.Name == keepNode})
	}
	return out, nil
}

// guestFromOrigin returns the guest id of an origin value ("g:<id>"), else 0.
func guestFromOrigin(v string) int64 {
	if id, ok := strings.CutPrefix(v, "g:"); ok {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// nodeFromOrigin returns the Proxmox node an origin value ("h:<addr>") names, else "".
func (s *Server) nodeFromOrigin(ctx context.Context, v string) string {
	addr, ok := strings.CutPrefix(v, "h:")
	if !ok || addr == "" {
		return ""
	}
	nodes, err := s.st.ListNodes(ctx)
	if err != nil {
		return ""
	}
	for _, n := range nodes {
		if n.Addr == addr {
			return n.Name
		}
	}
	return ""
}

// validType accepts "" (taken from the catalog icon) or a category id.
func validType(t string) bool {
	if t == "" {
		return true
	}
	for _, c := range catalog.Categories {
		if c == t {
			return true
		}
	}
	return false
}

// withPaths returns the groups with sub-groups named "Parent › Child", for display.
func withPaths(groups []store.Group) []store.Group {
	name := map[int64]string{}
	for _, g := range groups {
		name[g.ID] = g.Name
	}
	out := make([]store.Group, len(groups))
	copy(out, groups)
	for i, g := range out {
		if p, ok := name[g.ParentID]; ok && g.ParentID != 0 {
			out[i].Name = p + " › " + g.Name
		}
	}
	return out
}
