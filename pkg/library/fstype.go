package library

import (
	"log/slog"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/fsutil"
)

// checkFilesystem detects the filesystem type and enforces policy
func checkFilesystem(root string, allowSMB bool) error {
	fsType, err := fsutil.Check(root, allowSMB, "library root")
	if err != nil {
		return err
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
