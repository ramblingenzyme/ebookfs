// Package xml holds the rules that apply to an epub's XML wherever it is read
// or written: the whitespace and percent-decoding normalizations every
// attribute goes through, the resolution of an href against the document
// carrying it, and the namespace prefix a created element takes.
//
// The normalizations are types rather than helpers so that declaring a field is
// what applies the rule, leaving nothing to remember at each site. Href
// resolution has no field to hang off, so it stays a function.
package xml
