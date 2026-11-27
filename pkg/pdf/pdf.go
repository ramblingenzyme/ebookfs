// Package pdf reads and edits the document metadata of a PDF file: the title,
// authors, description, subjects, keywords, language, publisher, rights,
// contributors, sort title, author sort and series. It also reads the creation
// and publication dates and the identifiers.
//
// A PDF keeps metadata in two places: the document information dictionary
// (ISO 32000-2 §14.3.3) and the XMP metadata stream (§14.3.2). Each field is
// read from the XMP stream, falling back to the information dictionary, or
// from the dictionary first when its ModDate is later than the stream's
// MetadataDate, since then a tool that knew only the dictionary edited it
// last. Save writes every source that disagrees with the Book, so both agree
// afterwards.
//
// OpenFile opens only the file structure, which is enough to read its bytes.
// Open also reads the metadata, which reading or changing it needs.
//
// Save appends an incremental update (§7.5.6) rather than rewriting the file,
// so every byte of the original survives, followed by the changed objects.
//
// Values are reported exactly as the file states them. A document with no
// title has an empty Title rather than failing to open.
package pdf
