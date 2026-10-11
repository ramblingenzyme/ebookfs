package opds

import (
	"testing"
	"time"
)

func TestPublished(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2010-05-17T08:30:00+10:00", time.Date(2010, 5, 17, 8, 30, 0, 0, time.FixedZone("", 10*3600)), true},
		{"2010-05-17T08:30:00", time.Date(2010, 5, 17, 8, 30, 0, 0, time.UTC), true},
		{"2010-05-17", time.Date(2010, 5, 17, 0, 0, 0, 0, time.UTC), true},
		{"2010-05", time.Date(2010, 5, 1, 0, 0, 0, 0, time.UTC), true},
		{"2010", time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"May 2010", time.Time{}, false},
		{"", time.Time{}, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := published(tc.in)
			if ok != tc.ok || !got.Equal(tc.want) {
				t.Errorf("published(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
