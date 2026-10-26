package web

import "github.com/andregoncalvespires/homeview-dash/internal/i18n"

// Dashboard views. The choice is remembered per browser in a cookie.
const (
	viewGroups = "groups" // the admin's own groups
	viewInfra  = "infra"  // by machine, from Proxmox discovery
	viewType   = "type"   // by application category

	viewCookie = "hv_view"
)

func validView(v string) bool { return v == viewGroups || v == viewInfra || v == viewType }

type viewOpt struct {
	ID, Label string
	Current   bool
}

// viewMenu is the drop-down offered next to the gear on the dashboard.
type viewMenu struct{ Options []viewOpt }

func (s *Server) viewMenu(lang, cur string) *viewMenu {
	m := &viewMenu{}
	for _, id := range []string{viewGroups, viewInfra, viewType} {
		m.Options = append(m.Options, viewOpt{ID: id, Label: i18n.T(lang, "view."+id), Current: id == cur})
	}
	return m
}
