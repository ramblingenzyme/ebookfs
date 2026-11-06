package library_test

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestFilesystemDetectionLocal(t *testing.T) {
	// Test that we can open a library on a local filesystem
	cfg := testConfig(t)
	lib, err := library.Open(cfg)
	if err != nil {
		t.Fatalf("Open on local filesystem: %v", err)
	}
	lib.Close()
}

func TestWithAllowSMBOption(t *testing.T) {
	// Test that the option can be passed without error
	cfg := testConfig(t)
	lib, err := library.Open(cfg, library.WithAllowSMB())
	if err != nil {
		t.Fatalf("Open with WithAllowSMB: %v", err)
	}
	lib.Close()
}

// Note: Testing SMB refusal requires either mocking syscall.Statfs or running
// on an actual SMB filesystem, which is not practical in unit tests. The
// detection logic is tested indirectly by verifying that local filesystems
// work and that the option threads through correctly.
