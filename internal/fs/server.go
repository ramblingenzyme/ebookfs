package fs

import (
	"log"
	"os"

	"github.com/knusbaum/go9p"
	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/index"
	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/model"
	"github.com/ramblingenzyme/ebookfs/internal/store"
)

func StartServer(cfg *config.Config) {
	if err := os.MkdirAll(cfg.Library.Root, 0755); err != nil {
		log.Fatalf("creating library root: %v", err)
	}
	if err := os.MkdirAll(cfg.Library.InboxTemp, 0700); err != nil {
		log.Fatalf("creating inbox temp dir: %v", err)
	}

	idx, err := index.Open(cfg.Index.Path)
	if err != nil {
		log.Fatalf("opening index: %v", err)
	}

	lib := library.New(store.New(cfg.Library.Root, cfg.Library.InboxTemp), idx)

	// The store is the source of truth; rebuild the index from it on every start
	// so a stale or missing index can't leave the served tree out of sync.
	if err := lib.Reindex(); err != nil {
		log.Fatalf("reindexing library: %v", err)
	}

	// allBooks is set below after newFS returns, but createFile only fires once
	// a client writes to /inbox — well after startup completes.
	var allBooks *allBooksDir
	var byAuthor *byAuthorDir
	var byID *byIDDir
	var bySeries *bySeriesDir

	ebookfs, root := newFS(inboxCreateFile(lib, cfg.Library.InboxTemp, func(b *model.Book) {
		allBooks.add(b)
		byAuthor.add(b)
		byID.add(b)
		bySeries.add(b)
	}))

	registry := newBookRegistry(ebookfs, lib)

	books, err := lib.ListAll()
	if err != nil {
		log.Fatalf("loading books: %v", err)
	}
	allBooks = newAllBooksDir(ebookfs, registry, books)
	byAuthor = newByAuthorDir(ebookfs, registry, books)
	byID = newByIDDir(ebookfs, registry, books)
	bySeries = newBySeriesDir(ebookfs, registry, books)

	root.AddChild(newInboxDir(ebookfs))
	root.AddChild(allBooks)
	root.AddChild(byAuthor)
	root.AddChild(byID)
	root.AddChild(bySeries)

	log.Printf("serving 9P on %s", cfg.Server.Listen)
	go9p.Serve(cfg.Server.Listen, ebookfs.Server())
}
