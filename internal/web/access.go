package web

import "github.com/andregoncalvespires/homeview-dash/internal/store"

// visibleGroups returns the ids of the groups u may see. Administrators see
// everything. A restricted group is visible to the users listed on it, and a
// sub-group is only visible when its parent is too. Ungrouped services (group 0)
// are visible to everyone and are not part of this set.
func visibleGroups(groups []store.Group, u *store.User) map[int64]bool {
	out := make(map[int64]bool, len(groups))
	own := func(g store.Group) bool {
		if u.IsAdmin() || !g.Restricted {
			return true
		}
		for _, id := range g.UserIDs {
			if id == u.ID {
				return true
			}
		}
		return false
	}
	byID := make(map[int64]store.Group, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	for _, g := range groups {
		ok := own(g)
		if ok && g.ParentID != 0 {
			if p, found := byID[g.ParentID]; found {
				ok = own(p)
			}
		}
		out[g.ID] = ok
	}
	return out
}
