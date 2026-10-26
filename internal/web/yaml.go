package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
	"github.com/andregoncalvespires/homeview-dash/internal/yamlcfg"
)

// yamlPlan is a validated file, resolved against what exists.
type yamlPlan struct {
	groups   []planGroup
	ungroup  []planSvc
	delGroup []int64
	delSvc   []store.Service

	newGroups, updGroups, newSvcs, updSvcs int
}

type planGroup struct {
	g      store.Group // ID 0 when new
	parent int         // index of the parent in the plan, -1 for a top-level group
	svcs   []planSvc
}

type planSvc struct {
	s store.Service // ID 0 when new; GuestID kept from the existing one
}

func (p *yamlPlan) removed() (groups, services int) { return len(p.delGroup), len(p.delSvc) }

// exportYAML describes the current groups and services.
func (s *Server) exportYAML(ctx context.Context) (string, error) {
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		return "", err
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		return "", err
	}
	users, err := s.st.ListUsers(ctx)
	if err != nil {
		return "", err
	}
	email := map[int64]string{}
	for _, u := range users {
		email[u.ID] = u.Email
	}
	bySvc := map[int64][]yamlcfg.Service{}
	var loose []yamlcfg.Service
	known := map[int64]bool{}
	for _, g := range groups {
		known[g.ID] = true
	}
	for _, sv := range services {
		y := yamlcfg.Service{ID: sv.ID, Name: sv.Name, Description: sv.Description, Icon: sv.Icon, Type: sv.Type, InternalURL: sv.InternalURL, ExternalURL: sv.ExternalURL}
		if sv.GroupID != 0 && known[sv.GroupID] {
			bySvc[sv.GroupID] = append(bySvc[sv.GroupID], y)
		} else {
			loose = append(loose, y)
		}
	}
	conv := func(g store.Group) yamlcfg.Group {
		y := yamlcfg.Group{ID: g.ID, Name: g.Name, Restricted: g.Restricted, Services: bySvc[g.ID]}
		for _, uid := range g.UserIDs {
			if e, ok := email[uid]; ok {
				y.Users = append(y.Users, e)
			}
		}
		return y
	}
	doc := &yamlcfg.Doc{Ungrouped: loose}
	top := map[int64]int{}
	for _, g := range groups {
		if g.ParentID == 0 || !known[g.ParentID] {
			top[g.ID] = len(doc.Groups)
			doc.Groups = append(doc.Groups, conv(g))
		}
	}
	for _, g := range groups {
		if i, ok := top[g.ParentID]; ok && g.ParentID != 0 {
			doc.Groups[i].Groups = append(doc.Groups[i].Groups, conv(g))
		}
	}
	return yamlcfg.Marshal(doc), nil
}

type flatGroup struct {
	y      yamlcfg.Group
	parent int // index in the flat list, -1 for top level
}

// flatten lists every group of the document, each parent right before its sub-groups.
func flatten(doc *yamlcfg.Doc) []flatGroup {
	var out []flatGroup
	for _, g := range doc.Groups {
		i := len(out)
		out = append(out, flatGroup{y: g, parent: -1})
		for _, ch := range g.Groups {
			out = append(out, flatGroup{y: ch, parent: i})
		}
	}
	return out
}

// planYAML checks the document and decides what would change. The returned
// message is a user-facing validation error ("" when the file is fine).
func (s *Server) planYAML(ctx context.Context, lang string, doc *yamlcfg.Doc) (*yamlPlan, string, error) {
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		return nil, "", err
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		return nil, "", err
	}
	users, err := s.st.ListUsers(ctx)
	if err != nil {
		return nil, "", err
	}
	gByID := map[int64]store.Group{}
	for _, g := range groups {
		gByID[g.ID] = g
	}
	sByID := map[int64]store.Service{}
	for _, sv := range services {
		sByID[sv.ID] = sv
	}
	p := &yamlPlan{}
	flat := flatten(doc)

	// Groups: by id, then by name.
	usedG := map[int64]bool{}
	seen := map[string]bool{}
	match := make([]store.Group, len(flat))
	for i, fg := range flat {
		name, ok := store.CleanText(fg.y.Name, 80)
		if !ok || name == "" {
			return nil, fmt.Sprintf("%s #%d: %s", i18n.T(lang, "yaml.group"), i+1, i18n.T(lang, "err.name")), nil
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Sprintf("%s %q: %s", i18n.T(lang, "yaml.group"), name, i18n.T(lang, "err.yaml_dup")), nil
		}
		seen[strings.ToLower(name)] = true
		flat[i].y.Name = name
		if g, ok := gByID[fg.y.ID]; ok && fg.y.ID > 0 && !usedG[g.ID] {
			match[i], usedG[g.ID] = g, true
		}
	}
	for i, fg := range flat {
		if match[i].ID != 0 {
			continue
		}
		for _, g := range groups {
			if !usedG[g.ID] && strings.EqualFold(g.Name, fg.y.Name) {
				match[i], usedG[g.ID] = g, true
				break
			}
		}
	}
	for _, g := range groups {
		if !usedG[g.ID] {
			p.delGroup = append(p.delGroup, g.ID)
		}
	}

	// Who may see restricted groups (administrators always do, so they are skipped).
	byEmail := map[string]store.User{}
	for _, u := range users {
		byEmail[strings.ToLower(u.Email)] = u
	}
	access := make([][]int64, len(flat))
	for i, fg := range flat {
		if !fg.y.Restricted {
			continue
		}
		for _, e := range fg.y.Users {
			u, ok := byEmail[strings.ToLower(strings.TrimSpace(e))]
			if !ok {
				return nil, fmt.Sprintf("%s %q: %s %q", i18n.T(lang, "yaml.group"), fg.y.Name, i18n.T(lang, "yaml.nouser"), e), nil
			}
			if !u.IsAdmin() {
				access[i] = append(access[i], u.ID)
			}
		}
	}

	// Services: by id, then by name inside the same (existing) group.
	type slot struct {
		y     yamlcfg.Service
		group int // index into flat, -1 = ungrouped
	}
	var slots []slot
	for gi, fg := range flat {
		for _, y := range fg.y.Services {
			slots = append(slots, slot{y, gi})
		}
	}
	for _, y := range doc.Ungrouped {
		slots = append(slots, slot{y, -1})
	}
	usedS := map[int64]bool{}
	matched := make([]store.Service, len(slots))
	for i, sl := range slots {
		if sv, ok := sByID[sl.y.ID]; ok && sl.y.ID > 0 && !usedS[sv.ID] {
			matched[i], usedS[sv.ID] = sv, true
		}
	}
	for i, sl := range slots {
		if matched[i].ID != 0 {
			continue
		}
		var gid int64
		if sl.group >= 0 {
			if gid = match[sl.group].ID; gid == 0 {
				continue // a new group has no existing services
			}
		}
		for _, sv := range services {
			if !usedS[sv.ID] && sv.GroupID == gid && strings.EqualFold(sv.Name, strings.TrimSpace(sl.y.Name)) {
				matched[i], usedS[sv.ID] = sv, true
				break
			}
		}
	}
	for _, sv := range services {
		if !usedS[sv.ID] {
			p.delSvc = append(p.delSvc, sv)
		}
	}

	// Validate and assemble.
	built := make([]planSvc, len(slots))
	for i, sl := range slots {
		v := store.Service{
			ID: matched[i].ID, GuestID: matched[i].GuestID, Node: matched[i].Node,
			Name: sl.y.Name, Description: sl.y.Description, Icon: sl.y.Icon, Type: strings.TrimSpace(sl.y.Type),
			InternalURL: sl.y.InternalURL, ExternalURL: sl.y.ExternalURL,
		}
		if sl.group >= 0 {
			v.GroupID = match[sl.group].ID // new groups are filled in at apply time
		}
		if key := validateService(&v); key != "" {
			return nil, fmt.Sprintf("%s %q: %s", i18n.T(lang, "yaml.service"), strings.TrimSpace(sl.y.Name), i18n.T(lang, key)), nil
		}
		if _, up := uploadIconID(v.Icon); up && v.Icon != matched[i].Icon && !s.iconExists(ctx, v.Icon) {
			return nil, fmt.Sprintf("%s %q: %s", i18n.T(lang, "yaml.service"), v.Name, i18n.T(lang, "err.icon_gone")), nil
		}
		if m := matched[i]; m.ID == 0 {
			p.newSvcs++
		} else if m.Name != v.Name || m.Description != v.Description || m.Icon != v.Icon || m.Type != v.Type || m.InternalURL != v.InternalURL ||
			m.ExternalURL != v.ExternalURL || m.GroupID != v.GroupID {
			p.updSvcs++
		}
		built[i] = planSvc{s: v}
	}
	k := 0
	for gi, fg := range flat {
		pg := planGroup{g: match[gi], parent: fg.parent}
		if pg.g.ID == 0 {
			p.newGroups++
		} else if pg.g.Name != fg.y.Name || pg.g.Restricted != fg.y.Restricted || !sameIDs(pg.g.UserIDs, access[gi]) {
			p.updGroups++
		}
		pg.g.Name, pg.g.Restricted, pg.g.UserIDs = fg.y.Name, fg.y.Restricted, access[gi]
		for range fg.y.Services {
			pg.svcs = append(pg.svcs, built[k])
			k++
		}
		p.groups = append(p.groups, pg)
	}
	p.ungroup = built[k:]
	return p, "", nil
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[int64]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}

// applyYAML writes a validated plan: groups, then services, then removals.
func (s *Server) applyYAML(ctx context.Context, p *yamlPlan) error {
	pos := 0
	var oldIcons []string
	save := func(ps planSvc, gid int64) error {
		v := ps.s
		v.GroupID, v.Position = gid, pos
		pos++
		if v.ID != 0 {
			if cur, err := s.st.GetService(ctx, v.ID); err == nil && cur.Icon != v.Icon {
				oldIcons = append(oldIcons, cur.Icon)
			}
		}
		return s.st.SaveService(ctx, &v)
	}
	saved := make([]int64, len(p.groups)) // group id of each plan entry, once written
	for i := range p.groups {
		g := p.groups[i].g
		g.Position = i
		g.ParentID = 0
		if pi := p.groups[i].parent; pi >= 0 {
			g.ParentID = saved[pi]
		}
		if err := s.st.SaveGroup(ctx, &g); err != nil {
			return err
		}
		saved[i] = g.ID
		for _, ps := range p.groups[i].svcs {
			if err := save(ps, g.ID); err != nil {
				return err
			}
		}
	}
	for _, ps := range p.ungroup {
		if err := save(ps, 0); err != nil {
			return err
		}
	}
	for _, sv := range p.delSvc {
		if err := s.st.DeleteService(ctx, sv.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		oldIcons = append(oldIcons, sv.Icon)
	}
	for _, id := range p.delGroup {
		if err := s.st.DeleteGroup(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	for _, ic := range oldIcons {
		s.pruneIcon(ctx, ic)
	}
	return nil
}

type adminYAMLData struct {
	Text   string
	Notice string // validation error or the summary of a check
	Bad    bool
}

func (s *Server) renderYAML(w http.ResponseWriter, r *http.Request, sess *session, status int, d adminYAMLData) {
	s.render(w, r, sess, "admin_yaml", status, &page{TitleKey: "admin.yaml.title", Data: d})
}

func (s *Server) getAdminYAML(w http.ResponseWriter, r *http.Request, sess *session) {
	text, err := s.exportYAML(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	s.renderYAML(w, r, sess, http.StatusOK, adminYAMLData{Text: text})
}

func (s *Server) exportYAMLFile(w http.ResponseWriter, r *http.Request, sess *session) {
	text, err := s.exportYAML(r.Context())
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="homeview.yml"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(text))
}

const maxYAMLBytes = 512 << 10

func (s *Server) postAdminYAML(w http.ResponseWriter, r *http.Request, sess *session) {
	text := r.PostFormValue("yaml")
	lang := s.lang(r)
	d := adminYAMLData{Text: text}
	if len(text) > maxYAMLBytes {
		d.Notice, d.Bad = i18n.T(lang, "err.yaml_big"), true
		s.renderYAML(w, r, sess, http.StatusBadRequest, d)
		return
	}
	doc, err := yamlcfg.Unmarshal(text)
	if err != nil {
		d.Notice, d.Bad = i18n.T(lang, "yaml.invalid")+": "+err.Error(), true
		s.renderYAML(w, r, sess, http.StatusBadRequest, d)
		return
	}
	plan, msg, err := s.planYAML(r.Context(), lang, doc)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	if msg != "" {
		d.Notice, d.Bad = msg, true
		s.renderYAML(w, r, sess, http.StatusBadRequest, d)
		return
	}
	if r.PostFormValue("action") != "apply" {
		rg, rs := plan.removed()
		d.Notice = fmt.Sprintf(i18n.T(lang, "yaml.summary"), plan.newGroups, plan.updGroups, rg, plan.newSvcs, plan.updSvcs, rs)
		s.renderYAML(w, r, sess, http.StatusOK, d)
		return
	}
	if err := s.applyYAML(r.Context(), plan); err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/yaml?ok=msg.yaml_applied", http.StatusSeeOther)
}
