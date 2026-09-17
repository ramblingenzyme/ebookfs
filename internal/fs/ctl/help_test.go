package ctl

import (
	"strings"
	"testing"
)

// helpCommands extracts the command names the help file documents, reading the
// indented entries under "Commands:" up to the next unindented heading.
func helpCommands(t *testing.T) []string {
	t.Helper()
	_, rest, ok := strings.Cut(helpText, "Commands:\n")
	if !ok {
		t.Fatal("helpText has no Commands: section")
	}
	var names []string
	for line := range strings.SplitSeq(rest, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			break // next unindented heading
		}
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		t.Fatal("parsed no command names out of the help text")
	}
	return names
}

// Two of the three parallel lists the ctl surface is spread across: the dispatch
// switch in exec.go and this help text. Nothing in the types keeps them in step,
// so a command documented here but never wired reads as "unknown command" to
// the operator who follows the docs.
//
// The reverse direction (wired but undocumented) cannot be checked from here,
// since a Go switch is not enumerable.
func TestHelpDocumentsOnlyRealCommands(t *testing.T) {
	for _, name := range helpCommands(t) {
		t.Run(name, func(t *testing.T) {
			// No args, so every command answers with its usage line. What
			// matters is that dispatch recognises the name at all.
			got := dispatch(name, nil, nil, nil)
			if strings.HasPrefix(got, "error: unknown command") {
				t.Errorf("help documents %q but dispatch does not know it: %s", name, got)
			}
		})
	}
}

// A literal list that has to be edited when a command is added. It is a fourth
// copy of the same information, which is the point: it fails loudly rather than
// letting an undocumented command ship quietly.
func TestHelpCoversEveryDispatchedCommand(t *testing.T) {
	wired := []string{
		"add-tag", "remove-tag", "set-status", "set-rating",
		"delete", "rename-tag", "rename-author", "rename-series",
	}
	for _, name := range wired {
		if got := dispatch(name, nil, nil, nil); strings.HasPrefix(got, "error: unknown command") {
			t.Errorf("%q is in this test's list but dispatch does not know it; the list is stale", name)
		}
	}

	documented := helpCommands(t)
	for _, name := range wired {
		if !contains(documented, name) {
			t.Errorf("dispatch handles %q but the help file does not document it", name)
		}
	}
	for _, name := range documented {
		if !contains(wired, name) {
			t.Errorf("help documents %q but it is not in this test's wired list; either it was removed from dispatch or this list is stale", name)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
