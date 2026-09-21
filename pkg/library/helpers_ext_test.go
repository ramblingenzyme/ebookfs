// Helpers for the black-box tests in package library_test. A helper declared
// in package library is invisible here, and the white-box tests need the same
// five, so the public-API portion of helpers_test.go is duplicated rather than
// shared.
//
// The white-box-only helpers, drifted, breakEpub, dropIndex, metaPathOf and
// assertSettlesClean, stay in helpers_test.go.

package library_test

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

var makeBook = util.MakeMutableBook

func buildTestEpub(t *testing.T, title string, authors ...string) []byte {
	t.Helper()
	return util.BuildTestEpub(t, title, authors...)
}

func testConfig(t *testing.T) library.Config {
	t.Helper()
	return library.Config(util.TestConfig(t))
}

func openTestLibrary(t *testing.T) *library.Library {
	t.Helper()
	return openLib(t, testConfig(t))
}

// The black-box twin of helpers_test.go's openLib, which says what it is for.
func openLib(t *testing.T, cfg library.Config, opts ...library.Option) *library.Library {
	t.Helper()
	lib, err := library.Open(cfg, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

// The black-box twin of helpers_test.go's ingestTestEpub, which says why.
func ingestTestEpub(t *testing.T, lib *library.Library, data []byte) *library.Book {
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}
