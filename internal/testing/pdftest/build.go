// Package pdftest writes small PDFs for pkg/pdf's tests, in each of the file
// layouts a reader must handle. Values go in as raw PDF syntax, so a test can
// state exactly the bytes it means, escapes and encodings included.
package pdftest

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Layout is how the file locates its objects (ISO 32000-2 §7.5).
type Layout int

const (
	// Table is a §7.5.4 cross-reference table with uncompressed objects.
	Table Layout = iota
	// Stream puts every dictionary in a compressed object stream, indexed by a
	// cross-reference stream with a PNG predictor, as Acrobat and Ghostscript
	// write PDF 1.5 and later.
	Stream
)

// Doc describes a PDF to build.
type Doc struct {
	Layout Layout
	// Info holds the information dictionary's entries as raw PDF syntax, such
	// as {"Title": "(A Title)"}. Nil writes no information dictionary.
	Info map[string]string
	// XMP is the metadata stream's packet, or "" for none.
	XMP string
	// Catalog and Trailer are raw entries appended to those dictionaries,
	// such as "/AcroForm <</SigFlags 3>>".
	Catalog string
	Trailer string
}

// Object numbers, fixed so tests can name them.
const (
	catalogNum = 1
	pagesNum   = 2
	pageNum    = 3
	infoNum    = 4
	xmpNum     = 5
	objStmNum  = 6
	xrefNum    = 7
)

// Build writes doc to a temporary file and returns its path.
func Build(t *testing.T, doc Doc) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, Bytes(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// Bytes returns doc as a PDF file.
func Bytes(doc Doc) []byte {
	w := &writer{offsets: map[int]int{}}
	w.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")

	dicts := doc.dicts()
	if doc.XMP != "" {
		w.object(xmpNum, fmt.Sprintf("<</Type /Metadata /Subtype /XML /Length %d>>\nstream\n%s\nendstream", len(doc.XMP), doc.XMP))
	}

	trailer := fmt.Sprintf("/Root %d 0 R /ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>] %s", catalogNum, doc.Trailer)
	if doc.Info != nil {
		trailer += fmt.Sprintf(" /Info %d 0 R", infoNum)
	}

	switch doc.Layout {
	case Table:
		for _, num := range sortedKeys(dicts) {
			w.object(num, dicts[num])
		}

		w.table(xrefNum, trailer)
	case Stream:
		w.objStm(dicts)
		w.xrefStream(dicts, trailer)
	}

	return w.Bytes()
}

func (doc Doc) dicts() map[int]string {
	catalog := fmt.Sprintf("<</Type /Catalog /Pages %d 0 R %s", pagesNum, doc.Catalog)
	if doc.XMP != "" {
		catalog += fmt.Sprintf(" /Metadata %d 0 R", xmpNum)
	}

	dicts := map[int]string{
		catalogNum: catalog + ">>",
		pagesNum:   fmt.Sprintf("<</Type /Pages /Kids [%d 0 R] /Count 1>>", pageNum),
		pageNum:    fmt.Sprintf("<</Type /Page /Parent %d 0 R /MediaBox [0 0 612 792]>>", pagesNum),
	}

	if doc.Info != nil {
		var b strings.Builder

		b.WriteString("<<")

		for _, k := range sortedKeys(doc.Info) {
			fmt.Fprintf(&b, "/%s %s\n", k, doc.Info[k])
		}

		b.WriteString(">>")
		dicts[infoNum] = b.String()
	}

	return dicts
}

type writer struct {
	bytes.Buffer
	offsets map[int]int
}

func (w *writer) object(num int, body string) {
	w.offsets[num] = w.Len()
	fmt.Fprintf(w, "%d 0 obj\n%s\nendobj\n", num, body)
}

func (w *writer) table(size int, trailer string) {
	start := w.Len()
	fmt.Fprintf(w, "xref\n0 %d\n0000000000 65535 f\r\n", size)

	for num := 1; num < size; num++ {
		if off, ok := w.offsets[num]; ok {
			fmt.Fprintf(w, "%010d 00000 n\r\n", off)
		} else {
			w.WriteString("0000000000 00000 f\r\n")
		}
	}

	fmt.Fprintf(w, "trailer\n<</Size %d %s>>\nstartxref\n%d\n%%%%EOF\n", size, trailer, start)
}

func (w *writer) objStm(dicts map[int]string) {
	var header, body strings.Builder

	for _, num := range sortedKeys(dicts) {
		fmt.Fprintf(&header, "%d %d ", num, body.Len())
		body.WriteString(dicts[num] + "\n")
	}

	data := deflate([]byte(header.String() + body.String()))
	w.object(objStmNum, fmt.Sprintf("<</Type /ObjStm /N %d /First %d /Filter /FlateDecode /Length %d>>\nstream\n%s\nendstream",
		len(dicts), header.Len(), len(data), data))
}

// xrefStream writes entries of W [1 2 1], PNG-Up predicted, then deflated.
func (w *writer) xrefStream(dicts map[int]string, trailer string) {
	const columns = 4

	start := w.Len()
	w.offsets[xrefNum] = start

	rows := make([][]byte, xrefNum+1)
	rows[0] = []byte{0, 0, 0, 0}

	for num := 1; num <= xrefNum; num++ {
		_, compressed := dicts[num]
		off, uncompressed := w.offsets[num]

		switch {
		case compressed:
			rows[num] = []byte{2, 0, objStmNum, byte(indexIn(dicts, num))}
		case uncompressed:
			rows[num] = []byte{1, byte(off >> 8), byte(off), 0}
		default:
			rows[num] = []byte{0, 0, 0, 0}
		}
	}

	var data []byte

	prev := make([]byte, columns)

	for _, row := range rows {
		data = append(data, 2)
		for i := range row {
			data = append(data, row[i]-prev[i])
		}

		prev = row
	}

	data = deflate(data)
	fmt.Fprintf(w, "%d 0 obj\n<</Type /XRef /Size %d /W [1 2 1] /Filter /FlateDecode /DecodeParms <</Predictor 12 /Columns %d>> /Length %d %s>>\nstream\n%s\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n",
		xrefNum, xrefNum+1, columns, len(data), trailer, data, start)
}

func indexIn(dicts map[int]string, num int) int {
	return sort.SearchInts(sortedKeys(dicts), num)
}

func deflate(b []byte) []byte {
	var buf bytes.Buffer

	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()

	return buf.Bytes()
}

func sortedKeys[K int | string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// Packet wraps rdf:Description content in an XMP packet whose description
// declares the dc, xmp and pdf prefixes.
func Packet(properties string) string {
	return `<?xpacket begin="` + "\uFEFF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:dc="http://purl.org/dc/elements/1.1/"
  xmlns:xmp="http://ns.adobe.com/xap/1.0/"
  xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
` + properties + `
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
}
