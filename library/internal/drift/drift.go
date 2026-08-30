// Package drift holds the vocabulary of startup drift detection: the on-disk
// state of a book's files, as observed by the store and persisted by the index.
//
// It exists as its own package because both of those need the type and neither
// imports the other, and because the library package is the vocabulary the
// frontends share — this is internal bookkeeping they never see.
package drift

import "time"

// PathInfo carries the on-disk state of one book's files, observed with a single
// stat per file. Size accompanies the mtimes because mtime alone cannot detect a
// change made within the same clock tick as the recorded one — filesystems that
// stamp mtimes from the kernel's coarse clock (tmpfs among them) hand out
// identical nanosecond values for writes in the same tick.
//
// The zero PathInfo is the "unobserved" state, recorded for a book directory
// whose files could not be stat'd. It is a definite value rather than an absent
// one, so both sides of drift detection can record "we looked and could not see
// it" and agree with each other across restarts — otherwise one unreadable book
// means a full reindex on every startup, forever.
type PathInfo struct {
	Size      int64 // epub size, from the same stat as EpubMtime
	EpubMtime time.Time
	MetaSize  int64 // meta.toml size, from the same stat as MetaMtime
	MetaMtime time.Time
}

// IsUnobserved reports whether p records a failed observation rather than a
// real one. No stat of an existing file yields two zero mtimes.
func (p PathInfo) IsUnobserved() bool {
	return p.EpubMtime.IsZero() && p.MetaMtime.IsZero()
}

// Equal reports whether two observations describe the same on-disk state. The
// times need Time.Equal rather than ==, which also compares location and
// monotonic reading.
func (p PathInfo) Equal(o PathInfo) bool {
	return p.Size == o.Size && p.MetaSize == o.MetaSize &&
		p.EpubMtime.Equal(o.EpubMtime) &&
		p.MetaMtime.Equal(o.MetaMtime)
}
