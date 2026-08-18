// Package fs is the composition root of the 9P frontend: it wires the registry,
// views, and inbox subpackages onto a go9p filesystem and serves it. The
// building blocks live in fs/vfile, fs/book, fs/registry, fs/views, and
// fs/inbox; this package only assembles and starts them.
package fs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/ctl"
	"github.com/ramblingenzyme/ebookfs/fs/inbox"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/fs/views"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Server wraps a go9p.Server with lifecycle management.
type Server struct {
	ebookfs  *fs.FS
	root     *fs.StaticDir
	go9pSrv  *go9p.Server
	shutdown func() // closes frontend resources (search cleanup)
}

// Start begins serving the 9P filesystem on listen and blocks until
// Shutdown is called or the listener fails. It should be called from
// a background goroutine; the main goroutine handles signals.
func (s *Server) Start(listen string) error {
	slog.Info("serving 9P", "listen", listen)
	return s.go9pSrv.Serve(listen)
}

// Shutdown triggers graceful shutdown: frontend resources are closed first,
// then the 9P listener is closed and active connections are waited on with
// a deadline from ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.go9pSrv.Shutdown(ctx)
	s.shutdown()
	return err
}

// SetupServer wires the FS, registry, and views without starting the 9P
// listener, so the wiring can be tested without blocking.
func SetupServer(lib library.Library, exp library.Exporter, searchTTL time.Duration, searchMaxHandles int) (*Server, error) {
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

	books, err := lib.Search(model.Query{})
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
