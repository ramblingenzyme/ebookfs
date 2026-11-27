package pdf

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xmp"
)

// infoIsNewer reports whether the information dictionary was modified after
// the XMP packet, which happens when a tool that knows only the dictionary
// edits the file. Without both dates there is no evidence, so XMP wins.
func infoIsNewer(info cos.Dict, p *xmp.Packet) bool {
	mod, ok := info["ModDate"].(cos.String)
	if !ok {
		return false
	}

	infoTime, err := parsePDFDate(cos.Text(mod))
	if err != nil {
		return false
	}

	for _, prop := range []string{"MetadataDate", "ModifyDate"} {
		if v, ok := p.Values(xmpNS(prop)); ok && len(v) > 0 {
			t, err := parseXMPDate(v[0])

			return err == nil && infoTime.After(t)
		}
	}

	return false
}

// parsePDFDate reads §7.9.4's D:YYYYMMDDHHmmSSOHH'mm. Every field after the
// year is optional, and the apostrophe PDF 1.7 put after the minutes is
// accepted, as NOTE 2 recommends.
func parsePDFDate(s string) (time.Time, error) {
	s = strings.TrimPrefix(s, "D:")
	s = strings.TrimSuffix(s, "'")

	digits := len(s) - len(strings.TrimLeft(s, "0123456789"))
	if digits < 4 || digits > 14 || digits%2 != 0 {
		return time.Time{}, fmt.Errorf("date %q: want D:YYYY[MMDDHHmmSS]", s)
	}

	loc, err := pdfZone(s[digits:])
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q: %w", s, err)
	}

	// Missing fields take §7.9.4's defaults: month and day 1, the rest 0.
	// time.Date normalizes an out-of-range field, such as month 13, as
	// readers do, rather than rejecting the date.
	d := s[:digits] + "0101000000"[digits-4:]
	n := func(from, to int) int {
		v, _ := strconv.Atoi(d[from:to])

		return v
	}

	return time.Date(n(0, 4), time.Month(n(4, 6)), n(6, 8), n(8, 10), n(10, 12), n(12, 14), 0, loc), nil
}

// pdfZone reads the O HH'mm suffix. Without one, §7.9.4 says the time is UT.
func pdfZone(s string) (*time.Location, error) {
	if s == "" || s[0] == 'Z' {
		return time.UTC, nil
	}

	sign := map[byte]int{'+': 1, '-': -1}[s[0]]
	if sign == 0 {
		return nil, fmt.Errorf("time zone %q", s)
	}

	hh, mm, _ := strings.Cut(s[1:], "'")

	h, err := strconv.Atoi(hh)
	if err != nil {
		return nil, fmt.Errorf("time zone %q", s)
	}

	m, _ := strconv.Atoi(mm)

	return time.FixedZone("", sign*(h*3600+m*60)), nil
}

// xmpDateLayouts are the forms XMP Part 1 §8.2.1.1 allows, from the most
// precise down to a year alone.
var xmpDateLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02",
	"2006-01",
	"2006",
}

func parseXMPDate(s string) (time.Time, error) {
	for _, layout := range xmpDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("date %q is not ISO 8601", s)
}

// formatPDFDate writes §7.9.4's form, whose offset is Go's -07:00 with an
// apostrophe for the colon.
func formatPDFDate(t time.Time) string {
	return "D:" + t.Format("20060102150405") + strings.Replace(t.Format("-07:00"), ":", "'", 1)
}

func formatXMPDate(t time.Time) string { return t.Format(time.RFC3339) }
