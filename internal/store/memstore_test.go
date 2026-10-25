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
