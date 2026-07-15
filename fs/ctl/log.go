package ctl

import (
	"fmt"
	"sync"
	"time"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
)

// LogEntry records one command execution in the log.
type LogEntry struct {
	Timestamp time.Time
	Command   string
	Result    string
}

// CommandLog is an in-memory ring buffer of the last N command results. It is
// server-lifetime state shared across all 9P connections.
type CommandLog struct {
	mu      sync.Mutex
	entries []LogEntry
	max     int
	next    int
	full    bool
}

// NewCommandLog creates a ring buffer holding at most max entries.
func NewCommandLog(max int) *CommandLog {
	return &CommandLog{
		entries: make([]LogEntry, max),
		max:     max,
	}
}

// Append records one command result.
func (l *CommandLog) Append(cmd, result string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[l.next] = LogEntry{
		Timestamp: time.Now(),
		Command:   cmd,
		Result:    result,
	}
	l.next = (l.next + 1) % l.max
	if l.next == 0 {
		l.full = true
	}
}

// Entries returns a copy of all stored entries, oldest first.
func (l *CommandLog) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	var out []LogEntry
	if l.full {
		out = make([]LogEntry, l.max)
		copy(out, l.entries[l.next:])
		copy(out[l.max-l.next:], l.entries[:l.next])
	} else {
		out = make([]LogEntry, l.next)
		copy(out, l.entries[:l.next])
	}
	return out
}

// LogFile is a read-only 9P file that returns the command log contents.
type LogFile struct {
	vfile.SnapshotFile
}

// NewLogFile creates a read-only file named "log" that returns the command
// history on read.
func NewLogFile(f *fs.FS, cmdLog *CommandLog) *LogFile {
	return &LogFile{
		SnapshotFile: vfile.NewSnapshotFile(
			vfile.NewStat(f, "log", 0444),
			func() ([]byte, error) {
				entries := cmdLog.Entries()
				if len(entries) == 0 {
					return []byte("(no commands yet)\n"), nil
				}
				var buf []byte
				for _, e := range entries {
					buf = fmt.Appendf(buf, "%s  %-30s  %s\n",
						e.Timestamp.Format("2006-01-02 15:04:05"),
						e.Command,
						e.Result,
					)
				}
				return buf, nil
			},
		),
	}
}
