package store_test

import (
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
	"github.com/andregoncalvespires/homeview-dash/internal/store/storetest"
)

func TestMemStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return store.NewMemStore() })
}

func TestNormalizeEmail(t *testing.T) {
	if got := store.NormalizeEmail("  Ana@Example.COM "); got != "ana@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanText(t *testing.T) {
	ok := []string{"André Pires", "  Zoë  ", "日本語", "a", "Rua São João — nº 5"}
	for _, s := range ok {
		if _, good := store.CleanText(s, 80); !good {
			t.Errorf("rejected %q", s)
		}
	}
	if got, _ := store.CleanText("  André ", 80); got != "André" {
		t.Errorf("not trimmed: %q", got)
	}
	bad := []string{"Andr\xc3e Pires", "Andr\xe9 Pires", "a\x00b", "line\nbreak", "tab\there", "�", "x\x7f"}
	for _, s := range bad {
		if _, good := store.CleanText(s, 80); good {
			t.Errorf("accepted %q", s)
		}
	}
	if _, good := store.CleanText("áááá", 3); good {
		t.Error("length is measured in bytes, not characters")
	}
}
