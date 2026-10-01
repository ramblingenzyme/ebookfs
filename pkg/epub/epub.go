// Package epub reads and edits the metadata of an EPUB file: the title,
// authors, series, description, language, publisher, rights, subjects,
// contributors and publication date in the package document, and the cover image.
//
// OpenFile opens only the archive, which is enough to read its entries. Open
// also parses the package document, which reading or changing metadata needs.
//
// Save copies every entry it does not rewrite unchanged, and a rewritten entry
// keeps its namespace declarations, unknown metadata and CDATA sections. The
// archive keeps its entry order, and each entry its modification time and compression.
//
// Values are reported exactly as the file states them. A book with no title
// has an empty Title rather than failing to open.
package epub

// Author is a creator with the "aut" MARC relator, or with no role.
type Author struct {
	Name     string
	SortName string // "" if the file has none; never derived
}

// Contributor is a secondary participant in the book, such as an editor,
// translator, or illustrator. Role is a MARC relator code (e.g. "edt", "trl")
// or "" if the file has none.
type Contributor struct {
	Name string
	Role string
}

// Series is the collection a book belongs to, and its position in it. Index
// is a string because EPUB 3.3 D.3.7 allows positions such as "2.2.1". It is
// reported as written, even when D.3.7 does not allow it.
type Series struct {
	Name  string
	Index string
}
