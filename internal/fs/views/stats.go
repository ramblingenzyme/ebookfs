package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// StatsReader is the half of the library this package uses: the stats file is
// one aggregate read.
type StatsReader interface {
	Stats() (*library.Stats, error)
}

// statsFile is a read-only root file reporting aggregate library statistics.
// It has no state of its own. Every Open (and Stat, for an accurate length)
// re-derives content from lib.Stats, a live SQL aggregate over the index, so
// the file is always current.
type statsFile struct {
	vfile.SnapshotFile
	lib StatsReader
}

func NewStatsFile(f *fs.FS, lib StatsReader) *statsFile {
	sf := &statsFile{lib: lib}
	sf.SnapshotFile = vfile.NewSnapshotFile(newStat(f, "stats", 0444), sf.content)
	return sf
}

func (f *statsFile) content() ([]byte, error) {
	s, err := f.lib.Stats()
	if err != nil {
		return nil, err
	}
	return []byte(formatStats(s)), nil
}

// Stat runs the same SQL aggregate as content to report an accurate length,
// simple and always correct, but it means a bare `ls -l stats` costs a query,
// and a `stat` immediately followed by `open` (as most clients do) runs it
// twice. If that ever shows up in a profile, cache the formatted bytes for a
// short TTL, or invalidate via BookRegistry Add/Remove at the cost of this file
// registering as a BookView.
func (f *statsFile) Stat() proto.Stat {
	s := f.BaseFile.Stat()
	if data, err := f.content(); err == nil {
		s.Length = uint64(len(data))
	}
	return s
}

// formatStats renders s as newline-separated "key: value" lines, matching the
// plain-text convention every other field file in the tree uses. Timestamps
// print empty when zero (an empty library has no last-added/last-modified).
func formatStats(s *library.Stats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "books: %d\n", s.Books)
	fmt.Fprintf(&b, "authors: %d\n", s.Authors)
	fmt.Fprintf(&b, "series: %d\n", s.Series)
	fmt.Fprintf(&b, "tags: %d\n", s.Tags)
	fmt.Fprintf(&b, "total-size: %d\n", s.TotalSize)
	fmt.Fprintf(&b, "last-added: %s\n", formatTime(s.LastAdded))
	fmt.Fprintf(&b, "last-modified: %s\n", formatTime(s.LastModified))
	return b.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
