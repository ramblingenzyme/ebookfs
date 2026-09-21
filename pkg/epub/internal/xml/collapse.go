package xml

import "strings"

// Collapse is the whitespace normalization XML 1.0 §3.3.3 requires of a reader.
// Neither encoding/xml nor etree applies it.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
