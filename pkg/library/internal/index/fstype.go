package index

import (
	"fmt"
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

func (t FSType) IsSMB() bool {
	return t == FSTypeCIFS || t == FSTypeSMB2
}

// DetectFilesystemType returns the filesystem type at the given path
func DetectFilesystemType(path string) (FSType, error) {
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
