// Package syncutil holds small concurrency helpers shared by the library
// packages.
package syncutil

import "sync"

// KeyedMutex hands out one mutex per int64 key, created lazily on first use.
// Entries are never freed. The keys are book ids, so the map is bounded by the
// library's size.
//
// ponytail: free a mutex once no caller holds it, if KeyedMutex gets keys that
// are not bounded, or a library grows large enough to feel it.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

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
