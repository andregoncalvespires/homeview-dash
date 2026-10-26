package yamlcfg

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Service is one entry of the file. ID is 0 for a new service.
type Service struct {
	ID          int64
	Name        string
	Description string
	Icon        string
	InternalURL string
	ExternalURL string
}

// Group holds services in display order. ID is 0 for a new group.
type Group struct {
	ID       int64
	Name     string
	Services []Service
}

// Doc is the whole file: groups in order, then services without a group.
type Doc struct {
	Groups    []Group
	Ungrouped []Service
}

const Limit = 5000 // most services accepted in one file

// Header is written at the top of every export.
const Header = `# HomeView — grupos e serviços / groups and services
# 'id' liga cada item ao que já existe; sem 'id' o item é criado ou
# reconhecido pelo nome. Itens que não estão aqui serão removidos.
# 'id' ties an item to what exists; without it the item is created or
# matched by name. Items missing from this file are removed.
`

// Marshal writes d as YAML.
func Marshal(d *Doc) string {
	var b strings.Builder
	b.WriteString(Header)
	b.WriteString("\ngroups:")
	if len(d.Groups) == 0 {
		b.WriteString(" []")
	}
	b.WriteString("\n")
	for _, g := range d.Groups {
		if g.ID > 0 {
			fmt.Fprintf(&b, "  - id: %d\n    name: %s\n", g.ID, quote(g.Name))
		} else {
			fmt.Fprintf(&b, "  - name: %s\n", quote(g.Name))
		}
		writeServices(&b, g.Services, "    ")
	}
	b.WriteString("\nungrouped:")
	if len(d.Ungrouped) == 0 {
		b.WriteString(" []")
	}
	b.WriteString("\n")
	for _, s := range d.Ungrouped {
		writeService(&b, s, "  ")
	}
	return b.String()
}

func writeServices(b *strings.Builder, ss []Service, ind string) {
	if len(ss) == 0 {
		b.WriteString(ind + "services: []\n")
		return
	}
	b.WriteString(ind + "services:\n")
	for _, s := range ss {
		writeService(b, s, ind+"  ")
	}
}

func writeService(b *strings.Builder, s Service, ind string) {
	first := ind + "- "
	pad := ind + "  "
	if s.ID > 0 {
		fmt.Fprintf(b, "%sid: %d\n", first, s.ID)
		first = pad
	}
	fmt.Fprintf(b, "%sname: %s\n", first, quote(s.Name))
	if s.Description != "" {
		fmt.Fprintf(b, "%sdescription: %s\n", pad, quote(s.Description))
	}
	if s.Icon != "" {
		fmt.Fprintf(b, "%sicon: %s\n", pad, quote(s.Icon))
	}
	if s.InternalURL != "" {
		fmt.Fprintf(b, "%sinternal_url: %s\n", pad, quote(s.InternalURL))
	}
	if s.ExternalURL != "" {
		fmt.Fprintf(b, "%sexternal_url: %s\n", pad, quote(s.ExternalURL))
	}
}

// Unmarshal parses and shape-checks a document. Unknown keys are errors so a
// typo is caught instead of silently ignored.
func Unmarshal(src string) (*Doc, error) {
	root, err := parse(src)
	if err != nil {
		return nil, err
	}
	d := &Doc{}
	if root == nil {
		return d, nil
	}
	m, ok := root.(map[string]any)
	if !ok {
		return nil, errAt(0, "the file must start with \"groups:\" and/or \"ungrouped:\"")
	}
	if err := onlyKeys(m, "top level", "groups", "ungrouped"); err != nil {
		return nil, err
	}
	total := 0
	gl, err := list(m["groups"], "groups")
	if err != nil {
		return nil, err
	}
	for _, it := range gl {
		gm, ok := it.(map[string]any)
		if !ok {
			return nil, errAt(0, "each group must have a name")
		}
		if err := onlyKeys(gm, "group", "id", "name", "services"); err != nil {
			return nil, err
		}
		g := Group{}
		if g.ID, err = idOf(gm["id"], "group"); err != nil {
			return nil, err
		}
		if g.Name, err = str(gm["name"], "group name"); err != nil {
			return nil, err
		}
		sl, err := list(gm["services"], "services")
		if err != nil {
			return nil, err
		}
		for _, si := range sl {
			s, err := service(si)
			if err != nil {
				return nil, err
			}
			g.Services = append(g.Services, s)
			total++
		}
		d.Groups = append(d.Groups, g)
	}
	ul, err := list(m["ungrouped"], "ungrouped")
	if err != nil {
		return nil, err
	}
	for _, si := range ul {
		s, err := service(si)
		if err != nil {
			return nil, err
		}
		d.Ungrouped = append(d.Ungrouped, s)
		total++
	}
	if total > Limit {
		return nil, errAt(0, "too many services (max %d)", Limit)
	}
	return d, nil
}

func list(v any, what string) ([]any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case []any:
		return t, nil
	}
	return nil, errAt(0, "%q must be a list", what)
}

func service(v any) (Service, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Service{}, errAt(0, "each service needs \"name\" and a URL")
	}
	if err := onlyKeys(m, "service", "id", "name", "description", "icon", "internal_url", "external_url"); err != nil {
		return Service{}, err
	}
	var s Service
	var err error
	if s.ID, err = idOf(m["id"], "service"); err != nil {
		return s, err
	}
	for _, f := range []struct {
		dst *string
		key string
	}{{&s.Name, "name"}, {&s.Description, "description"}, {&s.Icon, "icon"}, {&s.InternalURL, "internal_url"}, {&s.ExternalURL, "external_url"}} {
		if *f.dst, err = str(m[f.key], f.key); err != nil {
			return s, err
		}
	}
	return s, nil
}

func str(v any, what string) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	}
	return "", errAt(0, "%q must be text", what)
}

func idOf(v any, what string) (int64, error) {
	s, err := str(v, what+" id")
	if err != nil || s == "" {
		return 0, err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, errAt(0, "%s id must be a positive number, got %q", what, s)
	}
	return n, nil
}

func onlyKeys(m map[string]any, where string, allowed ...string) error {
	var bad []string
	for k := range m {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
			}
		}
		if !found {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return errAt(0, "unknown key %q in %s (allowed: %s)", bad[0], where, strings.Join(allowed, ", "))
}
