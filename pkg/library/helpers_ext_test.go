// Helpers for the black-box tests in package library_test. They duplicate the
// public-API portion of helpers_test.go, which cannot be shared: a helper
// declared in package library is invisible here, and the white-box tests that
// stay in that package (drift, applymeta, the two error files) need the same
// five.
// The white-box-only helpers (drifted, breakEpub, dropIndex, metaPathOf,
// assertSettlesClean) are not duplicated and live with drift_test.go.

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

// openLib opens a library at cfg and registers its close, so an assertion that
// fails mid-test cannot leave the index open. Tests that reopen across a
// simulated restart still Close explicitly for sequencing; the second close is
// a no-op.
func openLib(t *testing.T, cfg library.Config, opts ...library.Option) *library.Library {
	t.Helper()
	lib, err := library.Open(cfg, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

// The black-box twin of helpers_test.go's ingestTestEpub, which says why the
// two cannot be one.
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
