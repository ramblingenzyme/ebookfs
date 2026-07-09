package fs

// Untested:
//   - StartServer() — calls go9p.Serve which blocks; wiring is verified
//     via setupServer() instead, which covers all setup logic.
//   - newBookDir one line inside the for-range loop over fields (88.9%).
//   - A few single-line branches in registry.edit — each one statement wide
//     and uninteresting.

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// The library-facade test doubles live in internal/testutil/libfake, and the
// simple book/FS helpers in internal/testutil. These aliases let the fs tests
// keep calling them unqualified.
var (
	makeBook  = testutil.MakeBook
	fixed     = testutil.Fixed
	errTest   = testutil.ErrTest
	newTestFS = testutil.NewTestFS
)

// newTestRegistry builds a registry over a fresh in-memory FS with no backing
// library. Tests that also need the FS directly can reach it via reg.f.
func newTestRegistry(t *testing.T) *bookRegistry {
	t.Helper()
	return newBookRegistry(newTestFS(t), nil)
}

func dirChildNames(d fs.Dir) []string {
	var names []string
	for name := range d.Children() {
		names = append(names, name)
	}
	return names
}

func firstDirChildNames(d fs.Dir) []string {
	for _, child := range d.Children() {
		if dir, ok := child.(fs.Dir); ok {
			return dirChildNames(dir)
		}
	}
	return nil
}

func protoDir(name string) *proto.Stat {
	return &proto.Stat{Name: name}
}
