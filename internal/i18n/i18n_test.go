package i18n

import (
	"strings"
	"testing"
)

func TestCatalogsHaveSameKeys(t *testing.T) {
	pt, en := Keys(PT), Keys(EN)
	if len(pt) != len(en) {
		t.Fatalf("pt has %d keys, en has %d", len(pt), len(en))
	}
	for i := range pt {
		if pt[i] != en[i] {
			t.Fatalf("key mismatch: pt %q vs en %q", pt[i], en[i])
		}
	}
	for _, l := range Languages {
		for _, k := range Keys(l) {
			if strings.TrimSpace(T(l, k)) == "" {
				t.Errorf("%s/%s is empty", l, k)
			}
		}
	}
}

func TestMailTemplatesFormat(t *testing.T) {
	for _, l := range Languages {
		if got := strings.Count(T(l, "mail.code.subject"), "%s"); got != 1 {
			t.Errorf("%s subject has %d %%s", l, got)
		}
		b := T(l, "mail.code.body")
		if strings.Count(b, "%s") != 2 || strings.Count(b, "%d") != 1 {
			t.Errorf("%s body placeholders wrong: %q", l, b)
		}
	}
}

func TestNormalizeAndMatch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"en", EN}, {"EN-us", EN}, {"en_GB", EN}, {"pt", PT}, {"pt-BR", PT}, {"fr", PT}, {"", PT},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	match := []struct{ header, def, want string }{
		{"en-US,en;q=0.9,pt;q=0.8", PT, EN},
		{"pt-BR,pt;q=0.9,en;q=0.8", EN, PT},
		{"fr-FR,fr;q=0.9,en;q=0.5", PT, EN},
		{"de,es", EN, EN},
		{"", PT, PT},
		{"*", EN, EN},
		{"en;q=0.3, pt;q=0.9", EN, PT},
	}
	for _, c := range match {
		if got := Match(c.header, c.def); got != c.want {
			t.Errorf("Match(%q, %q) = %q, want %q", c.header, c.def, got, c.want)
		}
	}
}

func TestFallbacks(t *testing.T) {
	if T(PT, "no.such.key") != "no.such.key" {
		t.Error("unknown key should return itself")
	}
	if !Has("login.title") || Has("nope") {
		t.Error("Has is wrong")
	}
}
