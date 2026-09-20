// Package xml holds the rules that apply to values read out of an epub's XML:
// the whitespace and percent-decoding normalizations every attribute goes
// through, and the resolution of an href against the document carrying it.
//
// The normalizations are types rather than helpers so that declaring a field is
// what applies the rule, leaving nothing to remember at each site. Href
// resolution has no field to hang off, so it stays a function.
package xml
