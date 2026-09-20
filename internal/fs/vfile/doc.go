// Package vfile holds the generic 9P file primitives: the snapshot and read-at
// base types that concrete files embed, plus the NewStat owner convention. It
// depends only on the library interfaces and plain-function callbacks, never on
// the book directory tree, registry, or views, so it is the leaf of the
// frontend.
package vfile
