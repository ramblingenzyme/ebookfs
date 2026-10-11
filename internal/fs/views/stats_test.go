package views

import (
	"strings"
	"testing"
	"time"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestFormatStats(t *testing.T) {
	added := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	modified := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)
	s := &library.Stats{
		Books: 3, Authors: 2, Series: 1, Tags: 4,
		TotalSize: 12345, LastAdded: added, LastModified: modified,
	}
	got := formatStats(s)
	for _, want := range []string{
		"books: 3\n",
		"authors: 2\n",
		"series: 1\n",
		"tags: 4\n",
		"total-size: 12345\n",
		"last-added: 2026-01-02T03:04:05Z\n",
		"last-modified: 2026-06-07T08:09:10Z\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatStats() = %q, want it to contain %q", got, want)
		}
	}

	zero := formatStats(&library.Stats{})
	for _, want := range []string{"last-added: \n", "last-modified: \n"} {
		if !strings.Contains(zero, want) {
			t.Errorf("formatStats() with zero times = %q, want it to contain %q", zero, want)
		}
	}
}

func TestStatsFile(t *testing.T) {
	t.Run("reads live stats", func(t *testing.T) {
		calls := 0
		lib := mock.StatsReader{
			StatsFn: func() (*library.Stats, error) {
				calls++
				return &library.Stats{Books: calls}, nil
			},
		}
		f := NewStatsFile(util.NewTestFS(t), lib)

		if got := fstest.Fid(t, f, 1).Get(proto.Mode(0), 1024); !strings.Contains(got, "books: 1\n") {
			t.Errorf("Read() = %q, want it to contain %q", got, "books: 1")
		}

		// A second Open re-derives content, observing the updated stats.
		if got := fstest.Fid(t, f, 2).Get(proto.Mode(0), 1024); !strings.Contains(got, "books: 2\n") {
			t.Errorf("second Read() = %q, want it to contain %q", got, "books: 2")
		}
	})

	t.Run("stat reports length", func(t *testing.T) {
		lib := mock.StatsReader{
			StatsFn: func() (*library.Stats, error) {
				return &library.Stats{Books: 7}, nil
			},
		}
		f := NewStatsFile(util.NewTestFS(t), lib)

		fstest.StatLength(t, f, uint64(len(formatStats(&library.Stats{Books: 7}))))
	})

	// A closed library fails Stats.
	t.Run("open propagates error", func(t *testing.T) {
		lib, err := library.Open(library.Config(util.TestConfig(t)))
		if err != nil {
			t.Fatal(err)
		}
		lib.Close()
		f := NewStatsFile(util.NewTestFS(t), lib)

		if err := f.Open(1, proto.Mode(0)); err == nil {
			t.Error("Open over a closed library succeeded, want the Stats error")
		}
	})
}
