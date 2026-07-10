package views

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

var (
	makeBook  = testutil.MakeBook
	newTestFS = testutil.NewTestFS
)

// newTestRegistry builds a registry over a fresh in-memory FS with no backing
// library, for driving views through their Add/Remove notifications.
func newTestRegistry(t *testing.T) *registry.BookRegistry {
	t.Helper()
	return registry.NewBookRegistry(newTestFS(t), nil)
}

func dirChildNames(d fs.Dir) []string {
	var names []string
	for name := range d.Children() {
		names = append(names, name)
	}
	return names
}
