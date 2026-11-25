package library_test

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestFilesystemDetectionLocal(t *testing.T) {
	cfg := testConfig(t)
	lib, err := library.Open(cfg)
	if err != nil {
		t.Fatalf("Open on local filesystem: %v", err)
	}
	lib.Close()
}

func TestWithAllowSMBOption(t *testing.T) {
	cfg := testConfig(t)
	lib, err := library.Open(cfg, library.WithAllowSMB())
	if err != nil {
		t.Fatalf("Open with WithAllowSMB: %v", err)
	}
	lib.Close()
}
