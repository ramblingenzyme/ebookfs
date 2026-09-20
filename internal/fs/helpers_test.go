package fs

// Server.Start is untested: it calls go9p.Serve, which blocks. SetupServer
// covers the wiring instead, and the e2e build tag covers a served tree.

import (
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

// The book and FS helpers live in internal/testutil. These aliases let the
// composition tests call them unqualified.
var (
	makeBook  = util.MakeMutableBook
	newTestFS = util.NewTestFS
	errTest   = util.ErrTest
)
