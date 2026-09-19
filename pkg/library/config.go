package library

// Config is the library's storage layout. All three paths are required. The
// library creates Root and InboxTemp if they are missing, and refuses to open
// unless InboxTemp is on the same filesystem as Root — ingest finalises by
// renaming out of one into the other, and rename does not cross filesystems.
//
// Deliberately free of serialization tags: how a caller obtains these paths —
// a TOML file, flags, environment — is the caller's concern, not the library's.
type Config struct {
	// Root is the library tree: one directory per author, one per book beneath.
	Root string
	// InboxTemp holds in-flight uploads until they are laid down under Root.
	InboxTemp string
	// IndexPath is the SQLite index file. The index is a derived cache of Root
	// (DECISIONS.md #2), so it may be deleted; it is rebuilt on the next open.
	IndexPath string
}

// ReaderConfig configures the export rendition served by Library.Exporter —
// the reader/ view an e-reader is synced from. Statuses selects which books
// appear; Convert toggles kepub conversion (false serves the original epub);
// CacheDir holds converted kepubs and MUST live outside Config.Root so the
// store walk never treats a cached file as a book.
type ReaderConfig struct {
	Statuses []string
	Convert  bool
	CacheDir string
}
