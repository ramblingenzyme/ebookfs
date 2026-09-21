package ctl

import (
	"fmt"
	"strings"
	"unicode"
)

// parseCommand splits a command line. Double quotes keep a tag or name with
// spaces in it as one argument.
func parseCommand(s string) (string, []string, error) {
	words, unterminated := splitWords(strings.TrimSpace(s))
	if unterminated {
		return "", nil, fmt.Errorf("unterminated double-quoted string")
	}
	if len(words) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	return words[0], words[1:], nil
}

// splitWords reports whether a quote was opened and never closed.
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
