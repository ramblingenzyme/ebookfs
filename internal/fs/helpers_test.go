package fs

// Server.Start is untested: it calls go9p.Serve, which blocks. SetupServer
// covers the wiring instead, and the e2e build tag covers a served tree.

import (
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// The library-facade test doubles live in internal/testutil/libfake, and the
// simple book/FS helpers in internal/testutil. These aliases let the composition
// tests call them unqualified.
var (
	makeBook  = testutil.MakeMutableBook
	newTestFS = testutil.NewTestFS
	errTest   = testutil.ErrTest
)
