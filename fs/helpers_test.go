package fs

// Untested:
//   - StartServer() — calls go9p.Serve which blocks; wiring is verified
//     via setupServer() instead, which covers all setup logic.

import (
	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// The library-facade test doubles live in internal/testutil/libfake, and the
// simple book/FS helpers in internal/testutil. These aliases let the composition
// tests call them unqualified.
var (
	makeBook  = book.MakeMutableBook
	newTestFS = testutil.NewTestFS
	errTest   = testutil.ErrTest
)
