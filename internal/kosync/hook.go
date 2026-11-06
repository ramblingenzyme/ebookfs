package kosync

import (
	"os"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// The Hook methods delegate to the registry, which owns the sidecar writes
// and the document-ID index this package doc comments describe.

func (s *Server) OnIngested(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	s.registry.Assign(book, sidecars, openEpub)
}

func (s *Server) OnEdited(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	s.registry.Refresh(book, sidecars, openEpub)
}

func (s *Server) OnDeleting(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	s.registry.Forget(book, sidecars)
}
