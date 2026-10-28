package library

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

const lockFileName = ".lock"

// acquireLock opens <root>/.lock and takes an exclusive flock.
// Returns the file handle, which must be kept open to hold the lock.
// If another process holds the lock, returns ErrLibraryLocked.
func acquireLock(root string) (*os.File, error) {
	path := filepath.Join(root, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			holder := readLockHolder(path)
			return nil, fmt.Errorf("%w: %s", ErrLibraryLocked, holder)
		}
		return nil, fmt.Errorf("flock lock file: %w", err)
	}

	writeLockHolder(f)

	if err := selfTestFlock(path); err != nil {
		f.Close()
		return nil, err
	}

	return f, nil
}

// selfTestFlock opens a second fd and tries to lock it. If the second lock
// succeeds, or if flock returns ENOLCK/ENOTSUP/EOPNOTSUPP, the filesystem
// doesn't enforce locks — warn and continue. Other errors fail Open.
func selfTestFlock(path string) error {
	fd2, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("self-test: open second fd: %w", err)
	}
	defer fd2.Close()

	err = syscall.Flock(int(fd2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		syscall.Flock(int(fd2.Fd()), syscall.LOCK_UN)
		slog.Error("flock is not enforced on this filesystem; a second instance could corrupt the library",
			"path", path)
		return nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return nil
	case errors.Is(err, syscall.ENOLCK), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP):
		slog.Error("flock is not supported on this filesystem; a second instance could corrupt the library",
			"path", path, "error", err)
		return nil
	default:
		return fmt.Errorf("self-test: flock: %w", err)
	}
}

// readLockHolder reads the lock file content, which is "<hostname>:<pid>".
func readLockHolder(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "unknown (lock file empty or unreadable)"
	}
	return string(data)
}

// writeLockHolder writes "<hostname>:<pid>" to the lock file. The flock is
// already held, so only the winner writes. Best-effort: if the write fails,
// the lock still works; the PID is informational for the next acquirer.
func writeLockHolder(f *os.File) {
	host, _ := os.Hostname()
	content := fmt.Sprintf("%s:%d", host, os.Getpid())
	if err := f.Truncate(0); err != nil {
		return
	}
	f.Seek(0, 0)
	f.Write([]byte(content))
}
