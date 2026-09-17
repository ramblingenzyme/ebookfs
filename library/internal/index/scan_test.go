package index

import (
	"testing"
	"time"
)

// A date the index cannot parse yields the zero time rather than a wrong one.
// The column is written by this package and should always be RFC3339, so this
// is the branch that fires when something else has written the row: a hand-edit,
// a restored backup, an older schema. Returning a parsed-looking date would put
// that book at an arbitrary point in every date ordering.
func TestParseDateFieldRejectsUnparseable(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want time.Time
	}{
		{"rfc3339", "2024-03-01T12:00:00Z", time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)},
		{"empty", "", time.Time{}},
		{"date only", "2024-03-01", time.Time{}},
		{"unix seconds", "1709294400", time.Time{}},
		{"not a date", "yesterday", time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDateField(tc.in, "date_added", 1)
			if !got.Equal(tc.want) {
				t.Errorf("parseDateField(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
