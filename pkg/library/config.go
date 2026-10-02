package library

// Config is the library's storage layout. All three paths are required. Open
// creates Root and InboxTemp if missing, and refuses to open unless both are
// on one filesystem, since ingest moves a book from one to the other by rename.
//
// It has no serialization tags; loading it is the caller's job.
type Config struct {
	// Root holds one directory per author, and one per book beneath.
	Root string
	// InboxTemp holds uploads until they are moved under Root.
	InboxTemp string
	// IndexPath is the SQLite index, a cache of Root (docs/DECISIONS.md #2). It
	// may be deleted; the next Open rebuilds it.
	IndexPath string
}

// ReaderConfig configures the rendition Library.Exporter serves to the reader/
// view an e-reader syncs from.
type ReaderConfig struct {
	// Statuses selects the books that appear.
	Statuses []string
	// Convert serves kepubs; false serves the original epub.
	Convert bool
}
