// Package xml holds the three rules an epub's XML obeys wherever it appears:
// the whitespace collapse a reader owes every value, the resolution of an href
// against the document carrying it, and the namespace prefix a created element
// takes.
//
// Each crosses a package boundary, which is what puts it here. A rule only one
// document format needs belongs with that format, as the container's attribute
// types do (internal/ocf).
package xml
