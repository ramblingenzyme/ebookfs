// Package fs is the composition root of the 9P frontend. It assembles the
// subpackages onto a go9p filesystem and serves it, and does nothing else.
package fs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/ctl"
	"github.com/ramblingenzyme/ebookfs/internal/fs/inbox"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type Server struct {
	ebookfs  *fs.FS
	root     *fs.StaticDir
	go9pSrv  *go9p.Server
	shutdown func() // closes frontend resources (search cleanup)
}

// Start blocks, so it runs in a goroutine and the main one takes signals.
func (s *Server) Start(listen string) error {
	slog.Info("serving 9P", "listen", listen)
	return s.go9pSrv.Serve(listen)
}

// Shutdown closes the listener and waits out active connections against ctx's
// deadline, then releases the frontend's own resources.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.go9pSrv.Shutdown(ctx)
	s.shutdown()
	return err
}

// Library is the union of what the frontend's parts declare. Nothing here
// names *library.Library, so a test drives the tree with a fake.
type Library interface {
	registry.Editor
	ctl.SearchDeleter
	inbox.Ingester
	views.StatsReader
}

// SetupServer wires everything without starting the listener, so the wiring
// can be tested without blocking.
func SetupServer(lib Library, exp library.Exporter, searchTTL time.Duration, searchMaxHandles int) (*Server, error) {
	ebookfs, root := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	reg := registry.NewBookRegistry(ebookfs, lib)
	ebookfs.CreateFile = vfile.DispatchCreate

	// Each view self-registers with the registry on construction.
	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)
	byID := views.NewByIDDir(reg)
	bySeries := views.NewBySeriesDir(reg)
	byTag := views.NewByTagDir(reg)
	byStatus := views.NewByStatusDir(reg)
	recent := views.NewRecentDir(reg)
	reader := views.NewReaderDir(reg, exp)
	stats := views.NewStatsFile(ebookfs, lib)

	books, err := lib.Search(library.Query{})
	if err != nil {
		return nil, fmt.Errorf("loading books: %w", err)
	}
	for _, b := range books {
		reg.Add(b)
	}

	root.AddChild(inbox.NewInboxDir(ebookfs, lib, reg.Add))
	root.AddChild(allBooks)
	root.AddChild(byAuthor)
	root.AddChild(byID)
	root.AddChild(bySeries)
	root.AddChild(byTag)
	root.AddChild(byStatus)
	root.AddChild(recent)
	root.AddChild(reader)
	root.AddChild(stats)

	cmdLog := ctl.NewCommandLog(100)
	root.AddChild(ctl.NewCtlFile(ebookfs, lib, reg, cmdLog))
	root.AddChild(ctl.NewLogFile(ebookfs, cmdLog))
	root.AddChild(ctl.NewHelpFile(ebookfs))

	search := views.NewSearchDir(ebookfs, reg, searchTTL, searchMaxHandles)
	root.AddChild(search)

	return &Server{
		ebookfs:  ebookfs,
		root:     root,
		go9pSrv:  go9p.NewServer(ebookfs.Server()),
		shutdown: search.Close,
	}, nil
}
