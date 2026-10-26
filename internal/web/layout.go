package web

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// layoutGroup is one group in a saved arrangement: the group (0 = the
// ungrouped section) and its services in display order.
type layoutGroup struct {
	Group    int64   `json:"group"`
	Services []int64 `json:"services"`
}

const maxLayoutItems = 5000

// postLayout stores a drag-and-drop arrangement of the dashboard. Only ids
// that exist are applied, unknown ones are ignored, and a service always
// ends up in exactly one group (its last mention wins).
func (s *Server) postLayout(w http.ResponseWriter, r *http.Request, sess *session) {
	var in []layoutGroup
	if err := json.Unmarshal([]byte(r.PostFormValue("layout")), &in); err != nil || len(in) > maxLayoutItems {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	n := 0
	for _, g := range in {
		n += len(g.Services)
	}
	if n > maxLayoutItems {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
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
	gByID := map[int64]int{}
	for i, g := range groups {
		gByID[g.ID] = i
	}
	sByID := map[int64]int{}
	for i, sv := range services {
		sByID[sv.ID] = i
	}

	// Resolve the arrangement first so a bad payload changes nothing.
	groupPos := map[int64]int{}
	svcGroup := map[int64]int64{}
	svcPos := map[int64]int{}
	pos := 0
	for _, lg := range in {
		if lg.Group != 0 {
			if _, ok := gByID[lg.Group]; !ok {
				continue
			}
			groupPos[lg.Group] = len(groupPos)
		}
		for _, id := range lg.Services {
			if _, ok := sByID[id]; !ok {
				continue
			}
			svcGroup[id], svcPos[id] = lg.Group, pos
			pos++
		}
	}

	for id, p := range groupPos {
		g := groups[gByID[id]]
		if g.Position == p {
			continue
		}
		g.Position = p
		if err := s.st.SaveGroup(ctx, &g); err != nil {
			s.serverError(w, r, sess, err)
			return
		}
	}
	for id, p := range svcPos {
		sv := services[sByID[id]]
		if sv.Position == p && sv.GroupID == svcGroup[id] {
			continue
		}
		sv.Position, sv.GroupID = p, svcGroup[id]
		if err := s.st.SaveService(ctx, &sv); err != nil {
			s.serverError(w, r, sess, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"groups":` + strconv.Itoa(len(groupPos)) + `,"services":` + strconv.Itoa(len(svcPos)) + `}`))
}
