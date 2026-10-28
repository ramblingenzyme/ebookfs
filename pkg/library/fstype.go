package library

import (
	"fmt"
	"log/slog"
	"syscall"
)

// Linux filesystem magic numbers from magic.h
const (
	nfsSuperMagic   = 0x6969
	cifsMagicNumber = 0xFF534D42 // "SMB" in ASCII
	smb2MagicNumber = 0xFE534D42
	fuseSuperMagic  = 0x65735546
)

// FSType represents the type of filesystem
type FSType int

const (
	FSTypeLocal FSType = iota
	FSTypeNFS
	FSTypeCIFS
	FSTypeSMB2
	FSTypeFUSE
	FSTypeUnknown
)

func (t FSType) String() string {
	switch t {
	case FSTypeLocal:
		return "local"
	case FSTypeNFS:
		return "NFS"
	case FSTypeCIFS:
		return "CIFS/SMB"
	case FSTypeSMB2:
		return "SMB2"
	case FSTypeFUSE:
		return "FUSE"
	default:
		return "unknown"
	}
}

// detectFilesystemType returns the filesystem type at the given path
func detectFilesystemType(path string) (FSType, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return FSTypeUnknown, fmt.Errorf("statfs %s: %w", path, err)
	}

	switch stat.Type {
	case nfsSuperMagic:
		return FSTypeNFS, nil
	case cifsMagicNumber:
		return FSTypeCIFS, nil
	case smb2MagicNumber:
		return FSTypeSMB2, nil
	case fuseSuperMagic:
		return FSTypeFUSE, nil
	default:
		// Treat anything else as local
		return FSTypeLocal, nil
	}
}

// checkFilesystem detects the filesystem type and enforces policy
func checkFilesystem(root string, allowSMB bool) error {
	fsType, err := detectFilesystemType(root)
	if err != nil {
		return err
	}

	// SMB variants are unreliable for atomic rename and locking
	if (fsType == FSTypeCIFS || fsType == FSTypeSMB2) && !allowSMB {
		return fmt.Errorf("SMB filesystem detected at %s: atomic rename and file locking are unreliable; use WithAllowSMB() to override if you understand the risks", root)
	}

	// Log the detection result
	if fsType == FSTypeCIFS || fsType == FSTypeSMB2 {
		// Only logged if allowSMB is true, otherwise we already returned an error
		slog.Warn("detected SMB filesystem, proceeding with override enabled", "path", root)
	} else {
		msg := "detected " + fsType.String() + " filesystem, using flock-based locking"
		slog.Info(msg, "path", root)
	}

	return nil
}
