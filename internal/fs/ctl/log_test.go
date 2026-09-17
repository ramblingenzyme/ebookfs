package ctl

import (
	"testing"
)

func TestCommandLog(t *testing.T) {
	log := NewCommandLog(3)

	if entries := log.Entries(); len(entries) != 0 {
		t.Fatalf("empty log entries = %d, want 0", len(entries))
	}

	log.Append("cmd1", "ok")
	log.Append("cmd2", "ok")
	log.Append("cmd3", "ok")

	entries := log.Entries()
	if len(entries) != 3 {
		t.Fatalf("log entries = %d, want 3", len(entries))
	}
	if entries[0].Command != "cmd1" {
		t.Errorf("first entry command = %q, want %q", entries[0].Command, "cmd1")
	}
	if entries[2].Command != "cmd3" {
		t.Errorf("third entry command = %q, want %q", entries[2].Command, "cmd3")
	}

	// Overflow: oldest entry ("cmd1") is evicted.
	log.Append("cmd4", "ok")
	entries = log.Entries()
	if len(entries) != 3 {
		t.Fatalf("overflow entries = %d, want 3", len(entries))
	}
	if entries[0].Command != "cmd2" {
		t.Errorf("after overflow, first entry command = %q, want %q", entries[0].Command, "cmd2")
	}
	if entries[2].Command != "cmd4" {
		t.Errorf("after overflow, last entry command = %q, want %q", entries[2].Command, "cmd4")
	}
}
