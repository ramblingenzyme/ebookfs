package fs

import (
	"fmt"
	"log"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/inbox"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/fs/views"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
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
	if err := go9p.Serve(listen, ebookfs.Server()); err != nil {
		log.Fatalf("9P server: %v", err)
	}
}

// setupServer wires the FS, registry, and views without starting the 9P
// listener, so the wiring can be tested without blocking. It returns the FS
// and the root directory for inspection.
func setupServer(lib library.Library, exp library.Exporter) (*fs.FS, *fs.StaticDir, error) {
	ebookfs, root := newFS()
	reg := registry.NewBookRegistry(ebookfs, lib)
	ebookfs.CreateFile = inbox.InboxCreateFile(lib, reg.Add)

	// TODO: add graceful shutdown. go9p.Serve blocks; the warmer's 4 goroutines
	// (reader.go) leak on exit because their channel is never closed.

	// Each view self-registers with the registry on construction.
	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)
	byID := views.NewByIDDir(reg)
	bySeries := views.NewBySeriesDir(reg)
	byTag := views.NewByTagDir(reg)
	byStatus := views.NewByStatusDir(reg)
	reader := views.NewReaderDir(reg, exp)

	books, err := lib.Query(model.Filter{})
	if err != nil {
		return nil, nil, fmt.Errorf("loading books: %w", err)
	}
	for _, b := range books {
		reg.Add(b)
	}

	root.AddChild(inbox.NewInboxDir(ebookfs))
	root.AddChild(allBooks)
	root.AddChild(byAuthor)
	root.AddChild(byID)
	root.AddChild(bySeries)
	root.AddChild(byTag)
	root.AddChild(byStatus)
	root.AddChild(reader)

	return ebookfs, root, nil
}
