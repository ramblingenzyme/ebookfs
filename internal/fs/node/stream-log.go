package node

import (
	"io"
	"sync"
)

// StreamLog is an append-only byte log. ReadAt blocks until data is available
// at the requested offset, enabling tail-f style reads.
type StreamLog struct {
	mu      sync.Mutex
	data    []byte
	waiters []chan struct{}
}

func (l *StreamLog) Append(line string) {
	l.mu.Lock()
	l.data = append(l.data, []byte(line)...)
	if len(line) == 0 || line[len(line)-1] != '\n' {
		l.data = append(l.data, '\n')
	}
	waiters := l.waiters
	l.waiters = nil
	l.mu.Unlock()
	for _, ch := range waiters {
		close(ch)
	}
}

func (l *StreamLog) ReadAt(buf []byte, offset int64, done <-chan struct{}) (int, error) {
	for {
		l.mu.Lock()
		if int64(len(l.data)) > offset {
			n := copy(buf, l.data[offset:])
			l.mu.Unlock()
			return n, nil
		}
		ch := make(chan struct{})
		l.waiters = append(l.waiters, ch)
		l.mu.Unlock()
		select {
		case <-ch:
		case <-done:
			return 0, io.EOF
		}
	}
}
