package yamlcfg

import (
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	d := &Doc{
		Groups: []Group{
			{ID: 3, Name: `Casa "smart" # 1`, Services: []Service{
				{ID: 7, Name: "Home Assistant", Description: "linha\ncom \\ barra", Icon: "app:homeassistant", InternalURL: "http://10.0.0.5:8123", ExternalURL: "https://ha.exemplo.com"},
				{Name: "Novo: teste", InternalURL: "http://x:1/a#b"},
			}},
			{Name: "Vazio"},
		},
		Ungrouped: []Service{{ID: 9, Name: "Solto ✓", Icon: "🏠", ExternalURL: "https://a.b"}},
	}
	got, err := Unmarshal(Marshal(d))
	if err != nil {
		t.Fatal(err)
	}
	// empty lists parse as nil
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("round trip differs:\n%+v\n%+v", got, d)
	}
}

func TestHandWritten(t *testing.T) {
	src := `
# meu arquivo
groups:
- name: Rede   # comentário
  services:
  - name: Router
    internal_url: http://192.168.0.1
    icon: 'it''s'
  - id: 4
    name: "Pi-hole"
    external_url: https://pihole.exemplo.com
ungrouped:
  - name: Solto
    internal_url: http://a:1
`
	d, err := Unmarshal(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Groups) != 1 || len(d.Groups[0].Services) != 2 || len(d.Ungrouped) != 1 {
		t.Fatalf("%+v", d)
	}
	s := d.Groups[0].Services
	if s[0].Icon != "it's" || s[0].InternalURL != "http://192.168.0.1" || s[1].ID != 4 || d.Groups[0].Name != "Rede" {
		t.Fatalf("%+v", s)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"tab":      "groups:\n\t- name: a\n",
		"unknown":  "groups:\n  - name: a\n    colour: red\n",
		"topkey":   "grupos: []\n",
		"badid":    "ungrouped:\n  - id: abc\n    name: a\n",
		"dupkey":   "groups: []\ngroups: []\n",
		"quote":    "ungrouped:\n  - name: \"abc\n",
		"flow":     "ungrouped: [a, b]\n",
		"notmap":   "- a\n",
		"badindnt": "groups:\n  - name: a\n   services: []\n",
		"nolist":   "groups: x\n",
	}
	for n, src := range cases {
		if _, err := Unmarshal(src); err == nil {
			t.Errorf("%s: expected error", n)
		}
	}
	if d, err := Unmarshal(""); err != nil || len(d.Groups) != 0 {
		t.Errorf("empty: %v", err)
	}
	if _, err := Unmarshal(strings.Repeat("a", 3) + "\n"); err == nil {
		t.Error("scalar document should fail")
	}
}
