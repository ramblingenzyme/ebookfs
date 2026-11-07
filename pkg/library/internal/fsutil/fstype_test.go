package fsutil

import (
	"errors"
	"testing"
)

// main.go matches this sentinel to name its --allow-smb flag.
func TestRequireNotSMB(t *testing.T) {
	for _, fsType := range []FSType{FSTypeCIFS, FSTypeSMB2} {
		if err := requireNotSMB(fsType, false, "/x"); !errors.Is(err, ErrSMBFilesystem) {
			t.Errorf("%v: err = %v, want ErrSMBFilesystem", fsType, err)
		}
		if err := requireNotSMB(fsType, true, "/x"); err != nil {
			t.Errorf("%v allowed: err = %v", fsType, err)
		}
	}
	if err := requireNotSMB(FSTypeNFS, false, "/x"); err != nil {
		t.Errorf("NFS: err = %v", err)
	}
}
