package library

import (
	"errors"
	"syscall"
	"testing"
)

func stubFlock(t *testing.T, err error) {
	t.Helper()
	prev := flock
	flock = func(int, int) error { return err }
	t.Cleanup(func() { flock = prev })
}

// NFSv3 without lockd answers the first flock with ENOLCK. Open warns and
// continues, as it does when the self-test's flock gets that error.
func TestOpenSurvivesUnsupportedFlock(t *testing.T) {
	stubFlock(t, syscall.ENOLCK)
	openTestLibrary(t)
}

func TestAcquireLockFailsOnOtherFlockError(t *testing.T) {
	stubFlock(t, syscall.EIO)
	if _, err := acquireLock(t.TempDir()); !errors.Is(err, syscall.EIO) {
		t.Fatalf("acquireLock err = %v, want EIO", err)
	}
}
