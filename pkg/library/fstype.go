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

	// ponytail: selfTestFlock runs on one host, so a lock local to this host
	// passes it. Parse /proc/self/mountinfo for nolock and local_lock to warn
	// only on the mounts affected, if the warning on every NFS start gets noisy.
	if fsType == fsutil.FSTypeNFS {
		slog.Warn("the flock self-test can't see other hosts; if this mount uses nolock or local_lock, a second host could open the library and corrupt it",
			"path", root)
	}

	return nil
}
