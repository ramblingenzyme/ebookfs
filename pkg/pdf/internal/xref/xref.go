// Package xref reads a PDF's file structure (ISO 32000-2 §7.5): the header, the
// chain of cross-reference sections and trailers, and the objects they locate,
// including those in object streams. It also appends incremental updates
// (§7.5.6).
//
// A file whose cross-reference data is wrong is rejected rather than repaired
// by scanning for objects, since an update appended to it would inherit the
// damage.
//
// ponytail: no repair at all, so a damaged file cannot even be read. Rebuild
// the table by scanning for "n g obj" when a real file needs it, and mark the
// result so Save refuses to append to it.
package xref

import (
	"bytes"
	"fmt"
	"io"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
)

// maxRefDepth bounds a chain of references to references. Real files have
// none; a cycle would otherwise loop forever.
const maxRefDepth = 32

// tailSize is how far from the end §7.5.5's startxref is searched for. Writers
// leave at most a few bytes after %%EOF, but some pad with a kilobyte of nulls.
const tailSize = 1024

type entry struct {
	kind byte  // 0 free, 1 in use, 2 in an object stream
	a    int64 // the byte offset, or the object stream's number
	b    int64 // the generation, or the index in the object stream
}

// Table is a PDF's merged cross-reference data. The newest section wins for
// each object number, as §7.5.6 says.
type Table struct {
	r    io.ReaderAt
	size int64

	// Trailer is the newest trailer. For a cross-reference stream it is the
	// stream's dictionary, which holds the same entries.
	Trailer cos.Dict

	startxref int64
	entries   map[int64]entry
	next      int64
	objStms   map[int64]*objStm
}

// Open reads the header and every cross-reference section. It resolves no
// objects, so it fails only on the file structure.
func Open(r io.ReaderAt, size int64) (*Table, error) {
	t := &Table{r: r, size: size, entries: map[int64]entry{}, objStms: map[int64]*objStm{}}

	// §7.5.2's header. Readers accept it within the first kilobyte, after junk
	// some mailers prepend, so this does too.
	head, err := t.read(0, min(size, tailSize))
	if err != nil {
		return nil, err
	}

	if !bytes.Contains(head, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w: no %%PDF- header", cos.ErrMalformed)
	}

	if t.startxref, err = t.findStartxref(); err != nil {
		return nil, err
	}

	if err := t.readSections(t.startxref); err != nil {
		return nil, err
	}

	if _, ok := t.Trailer["Root"].(cos.Ref); !ok {
		return nil, fmt.Errorf("%w: trailer has no Root reference", cos.ErrMalformed)
	}

	t.next, _ = cos.Int(t.Trailer["Size"])
	for num := range t.entries {
		t.next = max(t.next, num+1)
	}

	return t, nil
}

func (t *Table) findStartxref() (int64, error) {
	from := max(0, t.size-tailSize)

	tail, err := t.read(from, t.size-from)
	if err != nil {
		return 0, err
	}

	i := bytes.LastIndex(tail, []byte("startxref"))
	if i < 0 {
		return 0, fmt.Errorf("%w: no startxref", cos.ErrMalformed)
	}

	s := cos.NewScanner(t.r, t.size, from+int64(i)+int64(len("startxref")))

	off, err := s.Int()
	if err != nil {
		return 0, err
	}

	if off < 0 || off >= t.size {
		return 0, fmt.Errorf("%w: startxref %d is outside the file", cos.ErrMalformed, off)
	}

	return off, nil
}

// newRef allocates an object number no section uses.
func (t *Table) newRef() cos.Ref {
	t.next++

	return cos.Ref{Num: t.next - 1}
}

// Resolve follows o if it is a reference. A reference to a free or missing
// object resolves to null, as §7.3.10 says.
func (t *Table) Resolve(o cos.Object) (cos.Object, error) {
	for range maxRefDepth {
		ref, ok := o.(cos.Ref)
		if !ok {
			return o, nil
		}

		var err error
		if o, err = t.object(ref); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: references nested deeper than %d", cos.ErrMalformed, maxRefDepth)
}

// object returns null for a free or missing entry, and for a generation the
// entry does not have.
func (t *Table) object(ref cos.Ref) (cos.Object, error) {
	switch e := t.entries[ref.Num]; {
	case e.kind == 2 && ref.Gen == 0:
		return t.compressed(ref.Num, e.a, e.b)
	case e.kind == 1 && e.b == ref.Gen:
		return t.uncompressed(ref.Num, e.a)
	default:
		return nil, nil
	}
}

func (t *Table) uncompressed(num, off int64) (cos.Object, error) {
	got, o, err := cos.NewScanner(t.r, t.size, off).Indirect()
	if err != nil {
		return nil, fmt.Errorf("object %d at offset %d: %w", num, off, err)
	}

	if got.Num != num {
		return nil, fmt.Errorf("%w: cross-reference entry for object %d points at object %d", cos.ErrMalformed, num, got.Num)
	}

	return o, nil
}

// Data returns a stream's decoded data.
func (t *Table) Data(s *cos.Stream) ([]byte, error) {
	raw, err := t.raw(s)
	if err != nil {
		return nil, err
	}

	return cos.Decode(s.Dict, raw)
}

// raw reads a stream's encoded bytes. Length is often wrong in real files, so
// when "endstream" does not follow it, the data runs to the next "endstream"
// instead, as readers do.
func (t *Table) raw(s *cos.Stream) ([]byte, error) {
	lo, err := t.Resolve(s.Dict["Length"])
	if err != nil {
		return nil, err
	}

	n, ok := cos.Int(lo)
	if ok && n >= 0 && n <= cos.MaxDecoded && s.Offset+n <= t.size &&
		cos.NewScanner(t.r, t.size, s.Offset+n).AtKeyword("endstream") {
		return t.read(s.Offset, n)
	}

	return t.scanToEndstream(s.Offset)
}

func (t *Table) scanToEndstream(off int64) ([]byte, error) {
	data, err := t.read(off, min(t.size-off, cos.MaxDecoded))
	if err != nil {
		return nil, err
	}

	i := bytes.Index(data, []byte("endstream"))
	if i < 0 {
		return nil, fmt.Errorf("%w: stream at %d has no endstream", cos.ErrMalformed, off)
	}

	data = data[:i]
	data = bytes.TrimSuffix(data, []byte("\n"))
	data = bytes.TrimSuffix(data, []byte("\r"))

	return data, nil
}

func (t *Table) read(off, n int64) ([]byte, error) {
	buf := make([]byte, n)

	m, err := t.r.ReadAt(buf, off)
	if m == len(buf) {
		return buf, nil
	}

	if err == io.EOF {
		return nil, fmt.Errorf("%w: %d bytes at %d run past the end of the file", cos.ErrMalformed, n, off)
	}

	return nil, err
}
