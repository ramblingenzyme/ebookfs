package ctl

import (
	"strings"
	"testing"
)

// The help file is rendered from the commands table dispatch reads, so the two
// can no longer document different verbs. What is still worth pinning is the
// renderer: every command reaching the page, with the signature the operator
// will type and the description indented under it.
//
// This replaces a pair of tests that compared the help text against the
// dispatch switch and against a hand-kept list. Both were checking that three
// parallel lists agreed, and there is one list now.
func TestHelpRendersEveryCommand(t *testing.T) {
	help := helpText()
	for _, c := range commands {
		t.Run(c.name, func(t *testing.T) {
			signature := "  " + c.name + " " + c.params + "\n"
			if !strings.Contains(help, signature) {
				t.Errorf("help is missing the signature line %q", signature)
			}
			for line := range strings.SplitSeq(c.desc, "\n") {
				if !strings.Contains(help, "    "+line+"\n") {
					t.Errorf("help is missing description line %q", line)
				}
			}
		})
	}
}

// A usage line is what a command answers with when its arguments do not fit,
// and the same spelling the help file shows, so an operator who mistypes is
// told the form they can read about.
func TestUsageMatchesTheDocumentedSignature(t *testing.T) {
	for _, c := range commands {
		t.Run(c.name, func(t *testing.T) {
			got := dispatch(c.name, make([]string, c.arity()+1), nil, nil)
			if want := c.usage(); got != want {
				t.Errorf("wrong-arity result = %q, want %q", got, want)
			}
			if !strings.Contains(helpText(), got[len("usage: "):]) {
				t.Errorf("usage line %q does not appear in the help file", got)
			}
		})
	}
}
