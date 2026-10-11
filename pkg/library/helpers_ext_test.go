package library_test

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

var makeBook = util.MakeMutableBook

var buildTestEpub = util.BuildTestEpub

func testConfig(t *testing.T) library.Config {
	t.Helper()
	return library.Config(util.TestConfig(t))
}

func openTestLibrary(t *testing.T) *library.Library {
	t.Helper()
	return openLib(t, testConfig(t))
}

func openLib(t *testing.T, cfg library.Config, opts ...library.Option) *library.Library {
	t.Helper()
	lib, err := library.Open(cfg, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func ingestTestEpub(t *testing.T, lib *library.Library, data []byte) *library.Book {
	t.Helper()
	b, err := tryIngest(t, lib, data)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}

// tryIngest returns Ingest's error, and fails the test on any earlier step.
func tryIngest(t *testing.T, lib *library.Library, data []byte) (*library.Book, error) {
	//goland:noinspection DuplicatedCode
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	return h.Ingest()
}
