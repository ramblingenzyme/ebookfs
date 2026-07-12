package views

import (
	"strings"
	"testing"
	"time"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestFormatStats(t *testing.T) {
	added := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	modified := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)
	s := &model.Stats{
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
}

func TestFormatStatsZeroTimes(t *testing.T) {
	got := formatStats(&model.Stats{})
	if !strings.Contains(got, "last-added: \n") {
		t.Errorf("formatStats() with zero LastAdded = %q, want empty last-added", got)
	}
	if !strings.Contains(got, "last-modified: \n") {
		t.Errorf("formatStats() with zero LastModified = %q, want empty last-modified", got)
	}
}

func TestStatsFileReadsLiveStats(t *testing.T) {
	calls := 0
	lib := libfake.Lib{
		StatsFn: func() (*model.Stats, error) {
			calls++
			return &model.Stats{Books: calls}, nil
		},
	}
	f := NewStatsFile(testutil.NewTestFS(t), lib)

	if err := f.Open(1, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := f.Read(1, 0, 1024)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(data), "books: 1\n") {
		t.Errorf("Read() = %q, want it to contain %q", data, "books: 1")
	}

	// A second Open re-derives content, observing the updated stats.
	if err := f.Open(2, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err = f.Read(2, 0, 1024)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(data), "books: 2\n") {
		t.Errorf("second Read() = %q, want it to contain %q", data, "books: 2")
	}
}

func TestStatsFileStatReportsLength(t *testing.T) {
	lib := libfake.Lib{
		StatsFn: func() (*model.Stats, error) {
			return &model.Stats{Books: 7}, nil
		},
	}
	f := NewStatsFile(testutil.NewTestFS(t), lib)

	want := len(formatStats(&model.Stats{Books: 7}))
	if got := f.Stat().Length; got != uint64(want) {
		t.Errorf("Stat().Length = %d, want %d", got, want)
	}
}

func TestStatsFileOpenPropagatesError(t *testing.T) {
	lib := libfake.Lib{
		StatsFn: func() (*model.Stats, error) {
			return nil, testutil.ErrTest
		},
	}
	f := NewStatsFile(testutil.NewTestFS(t), lib)

	if err := f.Open(1, proto.Mode(0)); err != testutil.ErrTest {
		t.Errorf("Open error = %v, want %v", err, testutil.ErrTest)
	}
}
