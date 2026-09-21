package ctl

import (
	"strings"
	"testing"
)

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
			got := ctlFor(t).dispatch(c.name, make([]string, c.arity()+1))
			if want := c.usage(); got != want {
				t.Errorf("wrong-arity result = %q, want %q", got, want)
			}
			if !strings.Contains(helpText(), got[len("usage: "):]) {
				t.Errorf("usage line %q does not appear in the help file", got)
			}
		})
	}
}
