package kosync

import (
	"fmt"
	"os"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// The Hook methods delegate to the registry, which owns the sidecar writes
// and the document-ID index this package doc comments describe.

// Setup runs before AddHook registers the server, so no event reaches the
// index while Restore rebuilds it.
func (s *Server) Setup(root, stateDir string) error {
	if err := s.registry.Restore(s.lib); err != nil {
		return fmt.Errorf("restoring kosync index: %w", err)
	}
	return nil
}

func (s *Server) OnIngested(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	s.registry.Assign(book, sidecars, openEpub)
}

func (s *Server) OnEdited(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	s.registry.Refresh(book, sidecars, openEpub)
}

func (s *Server) OnDeleted(bookID int64) {
	s.registry.Forget(bookID)
}
