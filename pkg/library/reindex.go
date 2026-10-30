package library

import "github.com/ramblingenzyme/ebookfs/pkg/library/internal/reconcile"

// Reindex rebuilds the index from the store. A book that cannot be read is
// logged and skipped rather than failing the rebuild.
func (l *Library) Reindex() error {
	l.mutateMu.Lock()
	defer l.mutateMu.Unlock()
	return reconcile.Run(l.store, l.index, true)
}
