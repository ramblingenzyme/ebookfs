package drift

import (
	"testing"
	"time"
)

func observed() PathInfo {
	return PathInfo{
		Size:      4242,
		EpubMtime: time.Unix(1700000000, 123456789),
		MetaSize:  17,
		MetaMtime: time.Unix(1700000001, 987654321),
	}
}

func TestEqualComparesEveryField(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PathInfo)
	}{
		{"epub resized", func(p *PathInfo) { p.Size++ }},
		{"epub touched", func(p *PathInfo) { p.EpubMtime = p.EpubMtime.Add(time.Nanosecond) }},
		{"meta resized", func(p *PathInfo) { p.MetaSize++ }},
		{"meta touched", func(p *PathInfo) { p.MetaMtime = p.MetaMtime.Add(time.Nanosecond) }},
	}

	if base := observed(); !base.Equal(observed()) {
		t.Fatal("Equal = false for two identical observations")
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := observed()
			tc.mutate(&changed)
			if observed().Equal(changed) {
				t.Errorf("Equal = true despite %s; that change would never be detected as drift", tc.name)
			}
			if !changed.Equal(changed) {
				t.Error("Equal is not reflexive")
			}
		})
	}
}

// TestEqualComparesInstantsNotRepresentations is why Equal exists rather than
// ==. A time carries a location and, when it comes from time.Now, a monotonic
// reading; == compares those too, so a value that survived a round trip through
// the index would differ from the identical instant freshly stat'd and every
// startup would reindex.
func TestEqualComparesInstantsNotRepresentations(t *testing.T) {
	utc := observed()

	elsewhere := observed()
	elsewhere.EpubMtime = elsewhere.EpubMtime.In(time.FixedZone("UTC+10", 10*60*60))
	elsewhere.MetaMtime = elsewhere.MetaMtime.In(time.FixedZone("UTC-5", -5*60*60))
	if !utc.Equal(elsewhere) {
		t.Error("Equal = false for the same instants in a different location")
	}

	// Round(0) strips the monotonic reading a time.Now-derived value carries.
	now := time.Now()
	mono := PathInfo{EpubMtime: now, MetaMtime: now}
	wall := PathInfo{EpubMtime: now.Round(0), MetaMtime: now.Round(0)}
	if !mono.Equal(wall) {
		t.Error("Equal = false for the same instant with and without a monotonic reading")
	}
}

// TestIsUnobserved pins that a failed observation is recognised only when
// *both* files went unseen. A half-filled value is not a marker — treating one
// as such would index a book against file state that was never read.
func TestIsUnobserved(t *testing.T) {
	tests := []struct {
		name string
		pi   PathInfo
		want bool
	}{
		{"marker", Unobserved(), true},
		{"zero value", PathInfo{}, true},
		{"real observation", observed(), false},
		{"epub seen, meta not", PathInfo{EpubMtime: time.Unix(1, 0)}, false},
		{"meta seen, epub not", PathInfo{MetaMtime: time.Unix(1, 0)}, false},
		// Sizes alone say nothing: a stat that failed leaves them zero, and a
		// zero-length file is a real observation with a real mtime.
		{"sizes only", PathInfo{Size: 10, MetaSize: 10}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pi.IsUnobserved(); got != tc.want {
				t.Errorf("IsUnobserved() = %v, want %v", got, tc.want)
			}
		})
	}
}


