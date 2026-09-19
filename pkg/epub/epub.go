// Package epub reads and writes the metadata inside an EPUB file: the package
// document's title, authors, series, description, language and date, plus the
// cover image. It is a metadata editor, not a reader — nothing here renders a
// book or walks its spine.
//
// Two entry points, because parsing costs more than opening:
//
//	OpenFile — the zip, the mimetype check and the OCF container. Enough to
//	           read entries out of the archive, and nothing more.
//	Open     — the same, plus the parsed package document, which is what
//	           reading or changing metadata needs.
//
// A Book's metadata is exported struct fields. Assign one, call Save, and only
// the fields that moved are written. An exported field is something you can
// change; a method is something you can't, or something that costs I/O.
//
// Everything a file carries and this package does not model is preserved: an
// edit rewrites the entries it must and copies the rest byte for byte, keeping
// namespace declarations, foreign metadata, CDATA, entry order and compression
// method as they were. Values are reported as the file states them — no
// defaults are invented, and a book with no title reads back an empty Title
// rather than an error, because that is what the file says.
package epub

// Author is a creator the package document credits with writing the book:
// one carrying the "aut" MARC relator, or carrying no role at all. Editors,
// illustrators and translators are not authors here.
type Author struct {
	Name string
	// SortName is the name to file the author under ("Carroll, Lewis"), empty
	// when the file states none. Nothing here derives one.
	SortName string
}

// Series is a book's membership of a collection, and its position in it.
//
// Index is a string because EPUB 3.3 Appendix D.3.7 allows multi-level
// positions such as "2.2.1", which no number holds. It is reported exactly as
// the file states it, including empty and including values D.3.7 does not
// allow.
type Series struct {
	Name  string
	Index string
}
