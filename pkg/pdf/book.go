package pdf

import (
	"errors"
)

// ErrEncrypted is returned by Open for an encrypted PDF (§7.6), whose
// information dictionary strings cannot be read without decrypting them.
var ErrEncrypted = errors.New("pdf is encrypted")

// Metadata holds the editable document metadata of a PDF. It is embedded in
// Book so field access like b.Title works via promotion. An empty string or
// slice means the file has none, and setting one removes the field.
type Metadata struct {
	Title string
	// Authors is dc:creator, or the information dictionary's Author as a
	// single entry, since that is one string with no defined separator.
	Authors []string
	// Description is dc:description, or the information dictionary's
	// Subject, which §14.3.3 maps to it.
	Description string
	// Subjects is dc:subject, which has no information dictionary entry.
	Subjects []string
	// Keywords is pdf:Keywords or the information dictionary's Keywords: one
	// string, in whatever form the writer chose.
	Keywords string
	// Language is dc:language, or the catalog's Lang (§7.7.2).
	Language     string
	Publisher    string
	Rights       string
	Contributors []string
}

// Book is an open PDF with its metadata. Change a field and call Save to write
// it. A Book is not safe for concurrent use.
type Book struct {
	*File
	Metadata

	src sources
	// orig is a deep copy, so changing an exported slice cannot change what
	// Save compares against.
	orig Metadata
}

// Open opens the PDF at path and reads its metadata. A document is never
// rejected for what its metadata says, and an XMP packet that cannot be
// parsed is ignored until Save would have to rewrite it.
func Open(p string) (_ *Book, err error) {
	f, err := OpenFile(p)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	src, err := readSources(f)
	if err != nil {
		return nil, notPDF(p, err)
	}

	b := &Book{File: f, src: src}
	b.Metadata = build(src.read)
	b.orig = take(b.Metadata)

	return b, nil
}

// CreationDate returns the creation date exactly as the file writes it,
// chosen as the other fields are: xmp:CreateDate in ISO 8601 form, such as
// "2014-03-14T12:42:11+01:00", or the information dictionary's CreationDate in
// §7.9.4 form, such as "D:20140314124211+01'00". It is "" if the file has
// neither.
func (b *Book) CreationDate() string {
	vals := b.src.read(creationDate)
	if len(vals) == 0 {
		return ""
	}

	return vals[0]
}
