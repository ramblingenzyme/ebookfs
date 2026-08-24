// Package ncx writes the NCX: EPUB 2's table of contents (OPF 2.0 §2.4.1, which
// normatively defers to the DAISY/NISO Z39.86 §8 definition of the format;
// §5.9.5 keeps it as a legacy feature of EPUB 3, and packages still carry one
// for older reading systems). The zip container around it belongs to the parent
// epub package, and where in that container it lives is the package document's
// answer to give — opf.NCXPath.
//
// The NCX's <docTitle> and <docAuthor> are a second copy of the title and the
// authors the package document holds, and a reading system driving its
// navigation from the NCX shows those rather than the OPF's. Neither spec
// requires the two to agree, so keeping them in step is our rule rather than
// conformance: the package document stays the metadata of record, and nothing
// is ever read back out of here — a Bib comes from the OPF alone.
//
// Fields and slots are the package document's split, kept for the same reason:
// a field says what a value should be, a slot knows where the document records
// it. There is less of each here because there is less to say — every value
// this package touches sits in a <text> child, and none of them has an EPUB 2
// and an EPUB 3 spelling to choose between.
//
// Nothing is created that was not already there. A file with no <docTitle> has
// nothing to keep in step, and where a new one would have to go is Z39.86's
// content model to say, not ours.
package ncx

import (
	"bytes"
	"errors"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type Doc struct {
	doc *etree.Document
	ncx *etree.Element // <ncx>
}

// Parse rejects a document it cannot round-trip. etree refuses a malformed one
// either way since v1.7.0 — before that it corrected mismatched tags and
// truncation silently, and Apply would have written the correction back over a
// real table of contents. ValidateInput is what makes the refusal legible: it
// reports the syntax error and its line instead of a bare "invalid XML format",
// and that message is all the caller has to log.
//
// Strict here and permissive in opf.Parse is the difference between a write
// gate and a read gate. The caller skips an NCX this rejects, so strictness
// costs only the file it declines to touch; a package document rejected the
// same way is a book dropped from the library.
func Parse(b []byte) (*Doc, error) {
	doc := etree.NewDocument()
	doc.ReadSettings.ValidateInput = true
	// A navLabel may be wrapped in CDATA; opf.Parse says why that is preserved.
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

// Apply writes the two edits the NCX carries a copy of into the document and
// reports whether that changed anything; nothing is serialized until Bytes. No
// other field has an NCX representation.
func (d *Doc) Apply(e model.Edits) bool {
	before, _ := d.Bytes()

	if e.Title != nil {
		d.title().set(*e.Title)
	}
	if e.Authors != nil {
		d.authors().set(*e.Authors)
	}

	// Compared as serialized, the way opf.Doc.Apply does it: a field's set is
	// free to decide the document already carries the value, and only the bytes
	// know.
	after, _ := d.Bytes()
	return !bytes.Equal(before, after)
}

// title is the <docTitle>: the NCX's copy of the publication title.
func (d *Doc) title() textSlot { return slot(d.ncx.SelectElement("docTitle")) }

type authorsField struct{ d *Doc }

func (d *Doc) authors() authorsField { return authorsField{d} }

// set makes the NCX carry one <docAuthor> per author, in order — but only if it
// already carries at least one. An NCX naming no author is not disagreeing with
// the package document about who wrote the book.
//
// Extras go after the last existing one rather than at the end of <ncx>, whose
// content model fixes the order of its children (head, docTitle, docAuthor*,
// navMap, …); an existing sibling is a position already known to be right.
func (f authorsField) set(authors []model.Author) {
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

// A slot is one string value together with the place in the document that
// records it, so no field has to touch etree. The NCX has one such place: the
// <text> child that <docTitle> and <docAuthor> both wrap their value in.
//
// Write-only, unlike the package document's slots. Nothing reads metadata out
// of an NCX, so a get would have no read for the write to be kept honest by.
type textSlot struct{ owner *etree.Element }

func slot(owner *etree.Element) textSlot { return textSlot{owner: owner} }

// set is a no-op when the owner is absent, which is how a missing <docTitle>
// stays missing rather than being invented somewhere we would have to guess.
// The <text> child is created when the owner has none: Z39.86 makes it required
// in both elements, so a file without one is malformed and this is the repair.
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

// qualify puts a created element in the same namespace prefix as the element it
// sits with. The NCX namespace is normally the default one, leaving the prefix
// empty, but a file using an explicit prefix must not be given siblings in a
// different namespace.
func qualify(space, tag string) string {
	if space == "" {
		return tag
	}
	return space + ":" + tag
}
