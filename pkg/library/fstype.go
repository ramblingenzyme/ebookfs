package library

import (
	"log/slog"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/fsutil"
)

// checkFilesystem detects the filesystem type and enforces policy
func checkFilesystem(root string, allowSMB bool) error {
	fsType, err := fsutil.DetectFilesystemType(root)
	if err != nil {
		return err
	}

	if err := fsutil.RequireNotSMB(fsType, allowSMB, root); err != nil {
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
