package store

import (
	"github.com/ramblingenzyme/ebookfs/internal/epub"
)

// StoredBook is the filesystem representation of a book: its id, its location
// within the library tree, and the epub filename within that directory.
type StoredBook struct {
	ID           int64
	LibraryPath  string // relative to library root, e.g. "Le Guin, Ursula K/The Left Hand of Darkness (1042)"
	EpubFilename string // e.g. "The Left Hand of Darkness - Ursula K. Le Guin.epub"
}

// Store manages filesystem operations on the library tree.
type Store interface {
	// Ingest moves tmpPath into the canonical library location for book,
	// writes meta.toml with id and default sidecar values, and returns the
	// stored record. id must be pre-allocated by the caller (from the index).
	Ingest(id int64, book *epub.Book, tmpPath string) (*StoredBook, error)

	// ReadMeta reads the meta.toml sidecar for the given book.
	ReadMeta(b *StoredBook) (*Meta, error)

	// WriteMeta atomically replaces the meta.toml sidecar for the given book.
	WriteMeta(b *StoredBook, meta *Meta) error

	// Move renames the book directory to match newAuthor/newTitle, returning
	// an updated StoredBook. The caller is responsible for updating the index.
	Move(b *StoredBook, newAuthor epub.Author, newTitle string) (*StoredBook, error)

	// Delete removes the book directory from the library.
	Delete(b *StoredBook) error
}
