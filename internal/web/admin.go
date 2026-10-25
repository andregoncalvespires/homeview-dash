package web

import (
	"errors"
	"net/http"
	"net/mail"
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
	Edit   store.Service
	Groups []store.Group
	Rows   []serviceRow
	Apps   []catalog.App // catalog entries for the "application" picker
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
	names := map[int64]string{}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	data := adminServicesData{Edit: edit, Groups: groups, Apps: catalog.All()}
	for _, sv := range services {
		data.Rows = append(data.Rows, serviceRow{Service: sv, GroupName: names[sv.GroupID]})
	}
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
	if key := validateService(&v); key != "" {
		s.rerenderServiceForm(w, r, sess, v, key)
		return
	}
	if err := s.st.SaveService(r.Context(), &v); err != nil {
		if errors.Is(err, store.ErrNotFound) { // group (or the service itself) vanished
			s.rerenderServiceForm(w, r, sess, v, "err.group_missing")
			return
		}
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/services?ok=msg.saved", http.StatusSeeOther)
}

func (s *Server) rerenderServiceForm(w http.ResponseWriter, r *http.Request, sess *session, v store.Service, key string) {
	data, err := s.loadServicesPage(r, v)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
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
	if err := s.st.DeleteService(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/services?ok=msg.deleted", http.StatusSeeOther)
}

// ---- groups ----

type adminGroupsData struct{ Groups []store.Group }

func (s *Server) getAdminGroups(w http.ResponseWriter, r *http.Request, sess *session) {
	groups, err := s.st.ListGroups(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_groups", http.StatusOK, &page{TitleKey: "admin.groups.title", Data: adminGroupsData{Groups: groups}})
}

func (s *Server) saveGroup(w http.ResponseWriter, r *http.Request, sess *session) {
	name, ok := store.CleanText(r.PostFormValue("name"), 80)
	g := store.Group{Name: name, Position: formInt(r, "position")}
	if !ok || g.Name == "" {
		http.Redirect(w, r, "/admin/groups?err=err.name", http.StatusSeeOther)
		return
	}
	if id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64); err == nil && id > 0 {
		g.ID = id
	}
	if err := s.st.SaveGroup(r.Context(), &g); err != nil && !errors.Is(err, store.ErrNotFound) {
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

type adminUsersData struct{ Users []store.User }

func (s *Server) getAdminUsers(w http.ResponseWriter, r *http.Request, sess *session) {
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.render(w, r, sess, "admin_users", http.StatusOK, &page{TitleKey: "admin.users.title", Data: adminUsersData{Users: users}})
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
