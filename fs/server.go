package fs

import (
	"fmt"
	"log"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/library"
)

// StartServer serves the library over 9P at listen. The caller owns composition
// of the backend (store, index, library) and chooses the reader/ Exporter
// (original epub vs kepub); the frontend depends only on the library facade and
// that Exporter.
func StartServer(lib library.Library, exp library.Exporter, listen string) {
	ebookfs, _, err := setupServer(lib, exp)
	if err != nil {
		log.Fatalf("setting up server: %v", err)
	}
	log.Printf("serving 9P on %s", listen)
	go9p.Serve(listen, ebookfs.Server())
}

// setupServer wires the FS, registry, and views without starting the 9P
// listener, so the wiring can be tested without blocking. It returns the FS
// and the root directory for inspection.
func setupServer(lib library.Library, exp library.Exporter) (*fs.FS, *fs.StaticDir, error) {
	ebookfs, root := newFS()
	registry := newBookRegistry(ebookfs, lib)
	ebookfs.CreateFile = inboxCreateFile(lib, registry.Add)

	// TODO: add graceful shutdown. go9p.Serve blocks; the warmer's 4 goroutines
	// (reader.go) leak on exit because their channel is never closed.

	// Each view self-registers with the registry on construction.
	allBooks := newAllBooksDir(registry)
	byAuthor := newByAuthorDir(registry)
	byID := newByIDDir(registry)
	bySeries := newBySeriesDir(registry)
	reader := newReaderDir(registry, exp)

	books, err := lib.ListAll()
	if err != nil {
		return nil, nil, fmt.Errorf("loading books: %w", err)
	}
	for _, b := range books {
		registry.Add(b)
	}

	root.AddChild(newInboxDir(ebookfs))
	root.AddChild(allBooks)
	root.AddChild(byAuthor)
	root.AddChild(byID)
	root.AddChild(bySeries)
	root.AddChild(reader)

	return ebookfs, root, nil
}
