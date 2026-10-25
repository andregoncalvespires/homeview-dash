package sqlstore_test

import (
	"path/filepath"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
	"github.com/andregoncalvespires/homeview-dash/internal/store/sqlstore"
	"github.com/andregoncalvespires/homeview-dash/internal/store/storetest"
)

func TestSQLStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, err := sqlstore.Open(filepath.Join(t.TempDir(), "homeview.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "homeview.db")
	s, err := sqlstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(t.Context(), "k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = sqlstore.Open(path) // migrations must be idempotent
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, err := s.GetSetting(t.Context(), "k"); err != nil || v != "v" {
		t.Fatalf("got %q, %v", v, err)
	}
}
