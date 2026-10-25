package backgrounds_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/backgrounds"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func TestFilesExistAndDefaultIsListed(t *testing.T) {
	if backgrounds.Default != store.DefaultBgPreset || !backgrounds.Valid(backgrounds.Default) {
		t.Fatalf("default %q is not consistent", backgrounds.Default)
	}
	seen := map[string]bool{}
	for _, b := range backgrounds.All {
		if seen[b.ID] {
			t.Errorf("duplicate %q", b.ID)
		}
		seen[b.ID] = true
		for _, u := range []string{backgrounds.URL(b.ID), backgrounds.ThumbURL(b.ID)} {
			p := filepath.Join("..", "web", filepath.FromSlash(u[1:]))
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				t.Errorf("%s missing: %v", p, err)
			}
		}
	}
	for _, bad := range []string{"", "../etc/passwd", "oceano.webp", "Oceano"} {
		if backgrounds.Valid(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
