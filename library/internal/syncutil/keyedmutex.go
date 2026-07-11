// Package syncutil holds small concurrency helpers shared by the library
// packages.
package syncutil

import "sync"

// KeyedMutex hands out one mutex per int64 key, created lazily on first use.
// The zero value is ready to use. Entries are never freed: keys are book ids,
// whose count is small and bounded by the library size.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

// For returns the mutex for key, creating it on first use.
func (k *KeyedMutex) For(key int64) *sync.Mutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locks == nil {
		k.locks = make(map[int64]*sync.Mutex)
	}
	m, ok := k.locks[key]
	if !ok {
		m = &sync.Mutex{}
		k.locks[key] = m
	}
	return m
}
