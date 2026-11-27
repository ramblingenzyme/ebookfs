package xref

import (
	"fmt"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
)

// maxSections bounds the Prev chain. Each incremental update adds one section,
// and no real file has been saved a thousand times.
const maxSections = 1000

// readSections follows the Prev chain from the newest section. An entry already
// read is newer than the one found later, so it is kept.
func (t *Table) readSections(off int64) error {
	seen := map[int64]bool{}

	for {
		if seen[off] {
			return fmt.Errorf("%w: cross-reference sections loop at offset %d", cos.ErrMalformed, off)
		}

		if len(seen) == maxSections {
			return fmt.Errorf("%w: more than %d cross-reference sections", cos.ErrMalformed, maxSections)
		}

		seen[off] = true

		trailer, err := t.readSection(off)
		if err != nil {
			return err
		}

		if len(seen) == 1 {
			t.Trailer = trailer
		}

		prev, ok := cos.Int(trailer["Prev"])
		if !ok {
			return nil
		}

		off = prev
	}
}

func (t *Table) readSection(off int64) (cos.Dict, error) {
	s := cos.NewScanner(t.r, t.size, off)
	if !s.AtKeyword("xref") {
		return t.readStreamSection(off)
	}

	trailer, err := t.readTable(s)
	if err != nil {
		return nil, err
	}

	// §7.5.8.4: a hybrid file's stream entries rank below its table's but
	// above the previous section's.
	if stm, ok := cos.Int(trailer["XRefStm"]); ok {
		if _, err := t.readStreamSection(stm); err != nil {
			return nil, err
		}
	}

	return trailer, nil
}

// readTable reads a §7.5.4 table and the trailer after it. Entries are read
// as tokens rather than as fixed 20-byte lines, since some writers emit 19.
func (t *Table) readTable(s *cos.Scanner) (cos.Dict, error) {
	if err := s.Expect("xref"); err != nil {
		return nil, err
	}

	for !s.AtKeyword("trailer") {
		if err := t.readSubsection(s); err != nil {
			return nil, err
		}
	}

	if err := s.Expect("trailer"); err != nil {
		return nil, err
	}

	o, err := s.Object()
	if err != nil {
		return nil, err
	}

	d, ok := o.(cos.Dict)
	if !ok {
		return nil, fmt.Errorf("%w: trailer is not a dictionary", cos.ErrMalformed)
	}

	return d, nil
}

func (t *Table) readSubsection(s *cos.Scanner) error {
	h, err := s.Ints(2)
	if err != nil {
		return err
	}

	start, count := h[0], h[1]
	if start < 0 || count < 0 {
		return fmt.Errorf("%w: cross-reference subsection %d %d", cos.ErrMalformed, start, count)
	}

	for num := start; num < start+count; num++ {
		e, err := readEntry(s)
		if err != nil {
			return err
		}

		t.add(num, e)
	}

	return nil
}

func readEntry(s *cos.Scanner) (entry, error) {
	v, err := s.Ints(2)
	if err != nil {
		return entry{}, err
	}

	tok, err := s.Token()
	if err != nil {
		return entry{}, err
	}

	kind, ok := map[string]byte{"f": 0, "n": 1}[tok]
	if !ok {
		return entry{}, fmt.Errorf("%w: cross-reference entry type %q", cos.ErrMalformed, tok)
	}

	return entry{kind, v[0], v[1]}, nil
}

func (t *Table) add(num int64, e entry) {
	if _, ok := t.entries[num]; !ok {
		t.entries[num] = e
	}
}

// readStreamSection reads a §7.5.8 cross-reference stream.
func (t *Table) readStreamSection(off int64) (cos.Dict, error) {
	_, o, err := cos.NewScanner(t.r, t.size, off).Indirect()
	if err != nil {
		return nil, fmt.Errorf("cross-reference section at %d: %w", off, err)
	}

	s, ok := o.(*cos.Stream)
	if !ok || s.Dict["Type"] != cos.Name("XRef") {
		return nil, fmt.Errorf("%w: no cross-reference section at %d", cos.ErrMalformed, off)
	}

	data, err := t.Data(s)
	if err != nil {
		return nil, fmt.Errorf("cross-reference stream at %d: %w", off, err)
	}

	w, err := widths(s.Dict["W"])
	if err != nil {
		return nil, err
	}

	index, err := subsections(s.Dict)
	if err != nil {
		return nil, err
	}

	if err := t.addStreamEntries(data, w, index); err != nil {
		return nil, err
	}

	return s.Dict, nil
}

func widths(o cos.Object) ([3]int, error) {
	a, ok := ints(o)
	if !ok || len(a) < 3 || slices.Max(a[:3]) > 8 {
		return [3]int{}, fmt.Errorf("%w: cross-reference stream W %v", cos.ErrMalformed, o)
	}

	return [3]int{int(a[0]), int(a[1]), int(a[2])}, nil
}

// subsections returns Index as flat start, count pairs, defaulting to
// [0 Size].
func subsections(d cos.Dict) ([]int64, error) {
	if _, ok := d["Index"]; !ok {
		size, _ := cos.Int(d["Size"])

		return []int64{0, size}, nil
	}

	a, ok := ints(d["Index"])
	if !ok || len(a)%2 != 0 {
		return nil, fmt.Errorf("%w: cross-reference stream Index %v", cos.ErrMalformed, d["Index"])
	}

	return a, nil
}

// ints reads an array of non-negative integers.
func ints(o cos.Object) ([]int64, bool) {
	a, ok := o.(cos.Array)
	out := make([]int64, len(a))

	for i, v := range a {
		n, isInt := cos.Int(v)
		if !isInt || n < 0 {
			return nil, false
		}

		out[i] = n
	}

	return out, ok
}

func (t *Table) addStreamEntries(data []byte, w [3]int, index []int64) error {
	width := w[0] + w[1] + w[2]
	pos := 0

	for i := 0; i < len(index); i += 2 {
		for num := index[i]; num < index[i]+index[i+1]; num++ {
			if pos+width > len(data) {
				return fmt.Errorf("%w: cross-reference stream is shorter than its Index", cos.ErrMalformed)
			}

			row := data[pos : pos+width]
			pos += width

			kind := int64(1) // §7.5.8.2: a zero-width type field means type 1.
			if w[0] > 0 {
				kind = field(row[:w[0]])
			}

			a, b := field(row[w[0]:w[0]+w[1]]), field(row[w[0]+w[1]:])

			// §7.5.8.3: any other type is a reference to null, which a free
			// entry already means.
			if kind > 2 {
				kind = 0
			}

			t.add(num, entry{byte(kind), a, b})
		}
	}

	return nil
}

func field(b []byte) int64 {
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}

	return v
}
