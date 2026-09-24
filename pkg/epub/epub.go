// Package epub reads and writes the metadata inside an EPUB file: the package
// document's title, authors, series, description, language and date, plus the
// cover image.
//
// OpenFile opens the archive alone, enough to read entries. Open also parses
// the package document, which reading or changing metadata needs.
//
// A Book's exported fields are what Save can write back; a method is read-only
// or costs I/O. Save writes only the fields that moved and copies every other
// entry byte for byte. A rewritten entry keeps its namespace declarations,
// foreign metadata, CDATA, and the archive its entry order and compression.
// Values are reported as the file states them, so a book with no title reads
// back an empty Title rather than an error.
package epub

// Author is a creator carrying the "aut" MARC relator, or no role at all.
type Author struct {
	Name     string
	SortName string // "" when the file states none; nothing here derives one
}

// Series is a book's membership of a collection. Index is a string because
// EPUB 3.3 D.3.7 allows multi-level positions such as "2.2.1". It is reported
// as written, including values D.3.7 does not allow.
type Series struct {
	Name  string
	Index string
}
