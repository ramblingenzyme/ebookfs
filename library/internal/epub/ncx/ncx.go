// Package ncx writes the NCX: EPUB 2's table of contents (OPF 2.0 §2.4.1,
// which defers to DAISY/NISO Z39.86 §8; §5.9.5 keeps it as a legacy feature of
// EPUB 3, and packages still carry one). opf.NCXPath says where it lives.
//
// Its <docTitle> and <docAuthor> are a second copy of the title and authors,
// and a reading system navigating by the NCX shows those rather than the OPF's.
// Neither spec requires the two to agree, so keeping them in step is ours
// rather than conformance: the package document stays the metadata of record,
// and nothing is read back out of here.
//
// Nothing is created that was not already there: where a new element would go
// is Z39.86's content model to say, not ours.
package ncx

import (
	"bytes"
	"errors"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/edits"
)

type Doc struct {
	doc *etree.Document
	ncx *etree.Element // <ncx>
}

// Parse rejects a document it cannot round-trip, rather than writing back the
// correction etree would make; ValidateInput buys the syntax error and its line
// for the caller to log. Strict here and permissive in opf.Parse: the caller
// skips what this rejects, where a rejected package document is a book dropped
// from the library.
func Parse(b []byte) (*Doc, error) {
	doc := etree.NewDocument()
	doc.ReadSettings.ValidateInput = true
	// opf.Parse says why CDATA is preserved.
	doc.ReadSettings.PreserveCData = true
	if err := doc.ReadFromBytes(b); err != nil {
		return nil, err
	}
	ncx := doc.SelectElement("ncx")
	if ncx == nil {
		return nil, errors.New("no <ncx> element")
	}
	return &Doc{doc: doc, ncx: ncx}, nil
}

func (d *Doc) Bytes() ([]byte, error) { return d.doc.WriteToBytes() }

// Apply writes the title and authors — the only fields the NCX copies — and
// reports whether that changed anything. Nothing is serialized until Bytes.
func (d *Doc) Apply(e edits.Edits) bool {
	before, _ := d.Bytes()

	if e.Title != nil {
		d.title().set(*e.Title)
	}
	if e.Authors != nil {
		d.authors().set(*e.Authors)
	}

	// Serialized, as opf.Doc.Apply does: a set may find the value already there.
	after, _ := d.Bytes()
	return !bytes.Equal(before, after)
}

func (d *Doc) title() textSlot { return slot(d.ncx.SelectElement("docTitle")) }

type authorsField struct{ d *Doc }

func (d *Doc) authors() authorsField { return authorsField{d} }

// set makes the NCX carry one <docAuthor> per author, in order, but only if it
// already carries one: naming no author contradicts nothing. Extras go after
// the last rather than at the end of <ncx>, whose content model fixes the order
// of its children (head, docTitle, docAuthor*, navMap, …).
func (f authorsField) set(authors []book.Author) {
	existing := f.d.ncx.SelectElements("docAuthor")
	if len(existing) == 0 {
		return
	}

	last := existing[len(existing)-1]
	for i, a := range authors {
		if i < len(existing) {
			slot(existing[i]).set(a.Name)
			continue
		}
		el := etree.NewElement(qualify(last.Space, "docAuthor"))
		f.d.ncx.InsertChildAt(last.Index()+1, el)
		slot(el).set(a.Name)
		last = el
	}

	for i := len(authors); i < len(existing); i++ {
		f.d.ncx.RemoveChild(existing[i])
	}
}

// The <text> child <docTitle> and <docAuthor> wrap their value in — the only
// place the NCX records one. Write-only: nothing reads metadata out of an NCX.
type textSlot struct{ owner *etree.Element }

func slot(owner *etree.Element) textSlot { return textSlot{owner: owner} }

// set is a no-op when the owner is absent, so a missing <docTitle> stays missing
// rather than invented in a position we would have to guess. A missing <text>
// is created: Z39.86 requires it in both elements.
func (s textSlot) set(value string) {
	if s.owner == nil {
		return
	}
	t := s.owner.SelectElement("text")
	if t == nil {
		t = s.owner.CreateElement(qualify(s.owner.Space, "text"))
	}
	t.SetText(value)
}

// qualify puts a created element in its sibling's namespace prefix, normally
// the empty default, so a file using an explicit prefix keeps it.
func qualify(space, tag string) string {
	if space == "" {
		return tag
	}
	return space + ":" + tag
}
