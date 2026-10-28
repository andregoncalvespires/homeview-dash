package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// metricsMaxAge is how old a reading may be before the Proxmox servers are asked
// again. Several browsers share one reading.
const metricsMaxAge = 10 * time.Second

// pxLabels are the texts the dashboard script needs (keys of the px.* messages).
var pxLabels = []string{"cpu", "mem", "disk", "net", "up", "vcpu", "show", "hide", "all", "vms", "lxcs", "alerts",
	"none", "noread", "offline", "running", "stopped", "title", "alertn", "stale", "close", "machines", "vm", "lxc", "storage", "stmore", "stless"}

// metricsAccess returns what u may see. Administrators see everything.
func (s *Server) metricsAccess(ctx context.Context, u *store.User) store.MetricsAccess {
	if u.IsAdmin() {
		return store.MetricsAccess{UserID: u.ID, Nodes: true, Machines: true, Alerts: true}
	}
	a, err := s.st.GetMetricsAccess(ctx, u.ID)
	if err != nil {
		return store.MetricsAccess{UserID: u.ID}
	}
	return a
}

// pxPanel is the data of the dashboard's Proxmox row; nil hides it.
type pxPanel struct {
	Refresh int
	Labels  string // JSON object of texts
}

func (s *Server) pxPanel(ctx context.Context, u *store.User, lang string) *pxPanel {
	if s.disc == nil {
		return nil
	}
	a := s.metricsAccess(ctx, u)
	p, err := s.st.GetMetricsPrefs(ctx, u.ID)
	if err != nil || !a.Nodes || !p.Nodes {
		return nil
	}
	labels := make(map[string]string, len(pxLabels))
	for _, k := range pxLabels {
		labels[k] = i18n.T(lang, "px."+k)
	}
	b, _ := json.Marshal(labels)
	return &pxPanel{Refresh: p.Refresh, Labels: string(b)}
}

// ---- JSON for the dashboard row ----

type pxOpts struct {
	Alerts   bool `json:"alerts"`
	Machines bool `json:"machines"`
	CPU      bool `json:"cpu"`
	Mem      bool `json:"mem"`
	Disk     bool `json:"disk"`
	Net      bool `json:"net"`
	Warn     int  `json:"warn"`
	Crit     int  `json:"crit"`
	Refresh  int  `json:"refresh"`
}

type pxStorage struct {
	Name   string `json:"name"`
	Used   int64  `json:"used"`
	Total  int64  `json:"total"`
	Shared bool   `json:"shared,omitempty"`
}

type pxGuest struct {
	VMID    int      `json:"vmid"`
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Running bool     `json:"running"`
	CPU     float64  `json:"cpu"`
	CPUs    int      `json:"cpus"`
	Mem     [2]int64 `json:"mem"`
	Disk    [2]int64 `json:"disk"`
	In      float64  `json:"in"`
	Out     float64  `json:"out"`
}

type pxNode struct {
	Name     string      `json:"name"`
	Online   bool        `json:"online"`
	CPU      float64     `json:"cpu"`
	CPUs     int         `json:"cpus"`
	Mem      [2]int64    `json:"mem"`
	Uptime   int64       `json:"uptime"`
	Storages []pxStorage `json:"storages"`
	Guests   []pxGuest   `json:"guests"`
}

type pxConn struct {
	Name  string   `json:"name"`
	Err   string   `json:"err,omitempty"`
	At    int64    `json:"at"`
	Nodes []pxNode `json:"nodes"`
}

type pxResponse struct {
	Opts  pxOpts   `json:"opts"`
	Conns []pxConn `json:"conns"`
}

// hiddenGuests lists the machines a non-administrator must not see: those that
// back services, none of which the user can see. Machines without services are
// not hidden. The key is "connID/node/kind/vmid".
func (s *Server) hiddenGuests(ctx context.Context, u *store.User) (map[string]bool, error) {
	hidden := map[string]bool{}
	if u.IsAdmin() {
		return hidden, nil
	}
	groups, err := s.st.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	services, err := s.st.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	guests, err := s.st.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	visible := visibleGroups(groups, u)
	seen, shown := map[int64]bool{}, map[int64]bool{}
	for _, sv := range services {
		if sv.GuestID == 0 {
			continue
		}
		seen[sv.GuestID] = true
		if sv.GroupID == 0 || visible[sv.GroupID] {
			shown[sv.GuestID] = true
		}
	}
	for _, g := range guests {
		if seen[g.ID] && !shown[g.ID] {
			hidden[guestKey(g.ConnID, g.Node, g.Kind, g.VMID)] = true
		}
	}
	return hidden, nil
}

func guestKey(conn int64, node, kind string, vmid int) string {
	return fmt.Sprintf("%d/%s/%s/%d", conn, node, kind, vmid)
}

func (s *Server) getAPIProxmox(w http.ResponseWriter, r *http.Request, sess *session) {
	ctx := r.Context()
	a := s.metricsAccess(ctx, sess.User)
	p, err := s.st.GetMetricsPrefs(ctx, sess.User.ID)
	if err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	resp := pxResponse{Conns: []pxConn{}}
	resp.Opts = pxOpts{
		Alerts: a.Alerts && p.Alerts, Machines: a.Machines && p.Machines,
		CPU: p.CPU, Mem: p.Mem, Disk: p.Disk, Net: p.Net, Warn: p.Warn, Crit: p.Crit, Refresh: p.Refresh,
	}
	if s.disc != nil && a.Nodes && p.Nodes {
		var hidden map[string]bool
		if resp.Opts.Machines {
			if hidden, err = s.hiddenGuests(ctx, sess.User); err != nil {
				s.serverError(w, r, sess, err)
				return
			}
		}
		for _, rd := range s.disc.Readings(ctx, metricsMaxAge) {
			c := pxConn{Name: rd.ConnName, Err: rd.Err, At: rd.At.Unix(), Nodes: []pxNode{}}
			sharedSeen := map[string]bool{}
			for _, n := range rd.Nodes {
				pn := pxNode{Name: n.Name, Online: n.Online, CPU: n.CPU * 100, CPUs: n.MaxCPU,
					Mem: [2]int64{n.Mem, n.MaxMem}, Uptime: n.Uptime, Storages: []pxStorage{}, Guests: []pxGuest{}}
				for _, st := range rd.Storages {
					if st.Node != n.Name || (st.Shared && sharedSeen[st.Name]) {
						continue
					}
					if st.Shared {
						sharedSeen[st.Name] = true
					}
					pn.Storages = append(pn.Storages, pxStorage{Name: st.Name, Used: st.Used, Total: st.Total, Shared: st.Shared})
				}
				if resp.Opts.Machines {
					for _, g := range rd.Guests {
						if g.Node != n.Name || hidden[guestKey(rd.ConnID, g.Node, g.Kind, g.VMID)] {
							continue
						}
						pn.Guests = append(pn.Guests, pxGuest{VMID: g.VMID, Kind: g.Kind, Name: g.Name, Running: g.Running,
							CPU: g.CPU * 100, CPUs: g.MaxCPU, Mem: [2]int64{g.Mem, g.MaxMem}, Disk: [2]int64{g.Disk, g.MaxDisk},
							In: g.RateIn, Out: g.RateOut})
					}
				}
				c.Nodes = append(c.Nodes, pn)
			}
			resp.Conns = append(resp.Conns, c)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// ---- user preferences ----

type pxSettings struct {
	Access store.MetricsAccess
	Prefs  store.MetricsPrefs
}

func (s *Server) loadPxSettings(ctx context.Context, u *store.User) (pxSettings, error) {
	p, err := s.st.GetMetricsPrefs(ctx, u.ID)
	return pxSettings{Access: s.metricsAccess(ctx, u), Prefs: p}, err
}

func (s *Server) postPxPrefs(w http.ResponseWriter, r *http.Request, sess *session) {
	if !s.metricsAccess(r.Context(), sess.User).Any() {
		s.renderError(w, r, sess, http.StatusForbidden, "err.forbidden")
		return
	}
	on := func(name string) bool { return r.PostFormValue(name) != "" }
	num := func(name string) int {
		n, err := strconv.Atoi(r.PostFormValue(name))
		if err != nil {
			return -1
		}
		return n
	}
	p := store.MetricsPrefs{
		UserID: sess.User.ID, Nodes: on("nodes"), Machines: on("machines"), Alerts: on("alerts"),
		CPU: on("cpu"), Mem: on("mem"), Disk: on("disk"), Net: on("net"),
		Warn: num("warn"), Crit: num("crit"), Refresh: num("refresh"),
	}
	if !p.Valid() {
		http.Redirect(w, r, "/settings?err=err.px_range", http.StatusSeeOther)
		return
	}
	if err := s.st.SetMetricsPrefs(r.Context(), p); err != nil {
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/settings?ok=msg.prefs_saved", http.StatusSeeOther)
}

// ---- administrator: who may see what ----

func (s *Server) postAdminPxAccess(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := formID(r)
	if !ok {
		s.renderError(w, r, sess, http.StatusBadRequest, "err.badrequest")
		return
	}
	on := func(name string) bool { return r.PostFormValue(name) != "" }
	a := store.MetricsAccess{UserID: id, Nodes: on("nodes"), Machines: on("machines"), Alerts: on("alerts")}
	if err := s.st.SetMetricsAccess(r.Context(), a); err != nil {
		if err == store.ErrNotFound {
			s.renderError(w, r, sess, http.StatusNotFound, "err.notfound")
			return
		}
		s.serverError(w, r, sess, err)
		return
	}
	http.Redirect(w, r, "/admin/users?ok=msg.px_saved", http.StatusSeeOther)
}
