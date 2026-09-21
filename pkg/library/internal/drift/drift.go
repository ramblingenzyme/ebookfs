// Package drift holds the on-disk state of a book's files for startup drift
// detection. The store observes it; the index persists it.
package drift

import "time"

// Size is there because two writes in one clock tick share an mtime, as they
// do on tmpfs.
//
// The zero PathInfo means "stat'd, could not see it", recorded for a book
// directory whose files did not stat. Without a definite value for that, one
// unreadable book means a full reindex on every startup.
type PathInfo struct {
	Size      int64 // epub size, from the same stat as EpubMtime
	EpubMtime time.Time
	MetaSize  int64 // meta.toml size, from the same stat as MetaMtime
	MetaMtime time.Time
}

// No stat of an existing file yields two zero mtimes.
func (p PathInfo) IsUnobserved() bool {
	return p.EpubMtime.IsZero() && p.MetaMtime.IsZero()
}

// Equal uses Time.Equal rather than ==, which also compares location and
// monotonic reading.
func (p PathInfo) Equal(o PathInfo) bool {
	return p.Size == o.Size && p.MetaSize == o.MetaSize &&
		p.EpubMtime.Equal(o.EpubMtime) &&
		p.MetaMtime.Equal(o.MetaMtime)
}
