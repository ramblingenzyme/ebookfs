package library

import (
	"fmt"
	"log/slog"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
)

func requireNotSMB(fsType index.FSType, allowSMB bool, path string) error {
	if fsType.IsSMB() && !allowSMB {
		return fmt.Errorf("SMB filesystem detected at %s: atomic rename and file locking are unreliable; use WithAllowSMB() to override if you understand the risks", path)
	}
	return nil
}

// checkFilesystem detects the filesystem type and enforces policy
func checkFilesystem(root string, allowSMB bool) error {
	fsType, err := index.DetectFilesystemType(root)
	if err != nil {
		return err
	}

	if err := requireNotSMB(fsType, allowSMB, root); err != nil {
		return err
	}

	// Log the detection result
	if fsType.IsSMB() {
		// Only logged if allowSMB is true, otherwise we already returned an error
		slog.Warn("detected SMB filesystem, proceeding with override enabled", "path", root)
	} else {
		msg := "detected " + fsType.String() + " filesystem, using flock-based locking"
		slog.Info(msg, "path", root)
	}

	return nil
}
