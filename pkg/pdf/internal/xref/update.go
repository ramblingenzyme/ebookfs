package xref

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"math/bits"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
)

// sectionKeys belong to one cross-reference section, so an update's trailer
// never copies them from the previous one.
var sectionKeys = []cos.Name{"Prev", "Size", "XRefStm", "Type", "W", "Index", "Length", "Filter", "DecodeParms"}

// Update is a §7.5.6 incremental update being assembled: the objects it
// replaces or adds, and the trailer it ends with.
type Update struct {
	t    *Table
	objs map[cos.Ref]cos.Object
	// Trailer starts as a copy of the newest trailer. Write sets its Size
	// and Prev.
	Trailer cos.Dict
}

// Update starts an update to the file t was read from.
func (t *Table) Update() *Update {
	return &Update{t: t, objs: map[cos.Ref]cos.Object{}, Trailer: maps.Clone(t.Trailer)}
}

// Set replaces or adds the object ref names.
func (u *Update) Set(ref cos.Ref, o cos.Object) { u.objs[ref] = o }

// Put stores o in the object d[key] refers to. If d holds no reference there,
// Put allocates an object number, points d[key] at it, and reports true.
func (u *Update) Put(d cos.Dict, key cos.Name, o cos.Object) bool {
	ref, ok := d[key].(cos.Ref)
	if !ok {
		ref = u.t.newRef()
		d[key] = ref
	}

	u.objs[ref] = o

	return !ok
}

// Write writes the update to w, which already holds the original file.
//
// The section is a table or a stream, matching the newest one, since a reader
// that handles only tables cannot read a file that already uses streams.
func (u *Update) Write(w io.Writer) error {
	t := u.t

	e := &encoder{base: t.size}
	if last, err := t.read(t.size-1, 1); err != nil || last[0] != '\n' && last[0] != '\r' {
		e.buf = append(e.buf, '\n')
	}

	refs := slices.SortedFunc(maps.Keys(u.objs), func(a, b cos.Ref) int { return cmp.Compare(a.Num, b.Num) })
	for _, ref := range refs {
		e.written = append(e.written, written{ref, e.offset()})
		e.object(ref, u.objs[ref])
	}

	tr := maps.Clone(u.Trailer)
	for _, k := range sectionKeys {
		delete(tr, k)
	}

	// Only a cross-reference stream's dictionary has a Type, never a table's
	// trailer. A stream section lists itself, so its number counts toward Size.
	isStream := t.Trailer["Type"] == cos.Name("XRef")

	var self cos.Ref
	if isStream {
		self = t.newRef()
	}

	tr["Prev"], tr["Size"] = t.startxref, t.next

	if isStream {
		e.stream(self, tr)
	} else {
		e.table(tr)
	}

	_, err := w.Write(e.buf)

	return err
}

// encoder serializes an update's section after the original's last byte.
type encoder struct {
	base    int64
	buf     []byte
	written []written
}

type written struct {
	ref cos.Ref
	off int64
}

func (e *encoder) offset() int64 { return e.base + int64(len(e.buf)) }

func (e *encoder) object(ref cos.Ref, o cos.Object) {
	e.buf = fmt.Appendf(e.buf, "%d %d obj\n", ref.Num, ref.Gen)
	e.buf = cos.Append(e.buf, o)
	e.buf = append(e.buf, "\nendobj\n"...)
}

// runs groups the written objects into subsections of consecutive numbers.
func (e *encoder) runs() [][]written {
	var out [][]written

	for i, w := range e.written {
		if i > 0 && w.ref.Num == e.written[i-1].ref.Num+1 {
			out[len(out)-1] = append(out[len(out)-1], w)
		} else {
			out = append(out, []written{w})
		}
	}

	return out
}

func (e *encoder) table(trailer cos.Dict) {
	start := e.offset()

	e.buf = append(e.buf, "xref\n"...)
	for _, run := range e.runs() {
		e.buf = fmt.Appendf(e.buf, "%d %d\n", run[0].ref.Num, len(run))
		for _, w := range run {
			e.buf = fmt.Appendf(e.buf, "%010d %05d n\r\n", w.off, w.ref.Gen)
		}
	}

	e.buf = cos.Append(append(e.buf, "trailer\n"...), trailer)
	e.buf = append(e.buf, '\n')
	e.end(start)
}

func (e *encoder) end(xref int64) { e.buf = fmt.Appendf(e.buf, "startxref\n%d\n%%%%EOF\n", xref) }

// stream writes the section as an unfiltered cross-reference stream, which
// lists itself as §7.5.8.3 expects.
func (e *encoder) stream(self cos.Ref, d cos.Dict) {
	start := e.offset()
	e.written = append(e.written, written{self, start})
	offWidth := max(1, (bits.Len64(uint64(start))+7)/8)

	var data []byte

	index := cos.Array{}

	for _, run := range e.runs() {
		index = append(index, run[0].ref.Num, int64(len(run)))
		for _, w := range run {
			data = append(data, 1)
			data = appendField(data, w.off, offWidth)
			data = appendField(data, w.ref.Gen, 2)
		}
	}

	d["Type"] = cos.Name("XRef")
	d["W"] = cos.Array{int64(1), int64(offWidth), int64(2)}
	d["Index"] = index

	e.object(self, &cos.Stream{Dict: d, Data: data})
	e.end(start)
}

func appendField(b []byte, v int64, width int) []byte {
	for i := width - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*i)))
	}

	return b
}
