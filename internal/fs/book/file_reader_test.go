package book

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Stat reports zero for a size the exporter does not know, such as a kepub not
// converted yet, and rather than panic when the exporter is absent, the same
// case the read path handles with "exporter not available".
func TestReaderFileStat(t *testing.T) {
	lib, b := ingestedBook(t)
	kepub, err := lib.Exporter(library.ReaderConfig{Statuses: []string{"unread"}, Convert: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		exp  Renderer
	}{
		{"kepub not converted yet", kepub},
		{"no exporter", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rf := NewReaderFile(newStat(util.NewTestFS(t), "test.epub", 0444), tc.exp, util.Fixed(b))
			fstest.StatLength(t, rf, 0)
		})
	}
}
