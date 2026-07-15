package ctl

import (
	"fmt"
	"strings"
	"unicode"
)

// parsedCmd holds the result of parsing a command line.
type parsedCmd struct {
	name string
	args []string
}

// parseCommand splits a command line into name and arguments. Double-quoted
// strings are preserved as single arguments, letting tags and names contain
// spaces. Returns an error for empty input or unterminated quotes.
func parseCommand(s string) (parsedCmd, error) {
	words, unterminated := splitWords(strings.TrimSpace(s))
	if unterminated {
		return parsedCmd{}, fmt.Errorf("unterminated double-quoted string")
	}
	if len(words) == 0 {
		return parsedCmd{}, fmt.Errorf("empty command")
	}
	return parsedCmd{name: words[0], args: words[1:]}, nil
}

// splitWords splits s into words on whitespace boundaries, respecting
// double-quoted strings. The bool return reports whether a quote was
// opened but never closed.
func splitWords(s string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	inQuote := false

	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words, inQuote
}
