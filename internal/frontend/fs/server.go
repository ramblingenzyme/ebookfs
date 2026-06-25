package fs

import (
	"log"

	"github.com/knusbaum/go9p"
	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// StartServer serves the library over 9P at listen. The caller owns composition
// of the backend (store, index, library) and chooses the reader/ Exporter
// (original epub vs kepub); the frontend depends only on the library facade and
// that Exporter. inboxTemp is where uploads are staged before ingest.
func StartServer(lib *library.Library, exp Exporter, readerCfg ReaderConfig, listen, inboxTemp string) {
	// registry is set below after newFS returns, but createFile only fires once
	// a client writes to /inbox — well after startup completes.
	var registry *bookRegistry

	ebookfs, root := newFS(inboxCreateFile(lib, inboxTemp, func(b *model.Book) {
		registry.Add(b)
	}))

	registry = newBookRegistry(ebookfs, lib)

	// Each view self-registers with the registry on construction.
	allBooks := newAllBooksDir(registry)
	byAuthor := newByAuthorDir(registry)
	byID := newByIDDir(registry)
	bySeries := newBySeriesDir(registry)
	reader := newReaderDir(registry, exp, readerCfg)

	books, err := lib.ListAll()
	if err != nil {
		log.Fatalf("loading books: %v", err)
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

	log.Printf("serving 9P on %s", listen)
	go9p.Serve(listen, ebookfs.Server())
}
