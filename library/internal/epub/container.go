package epub

import (
	"encoding/xml"
	"io"
)

// Reading META-INF/container.xml, the file that says where the package document
// is. Everything here is about what the container declares; whether the archive
// actually holds what it names is zip.go's question, and the two errors that
// distinction produces land on opposite sides of it — ErrNoRootfile means the
// file declared none, ErrRootfileMissing means it named one the zip lacks.

const (
	containerPath = "META-INF/container.xml"
	metadataType  = "application/oebps-package+xml"
)

// container is the entry point to the epub. Its rootfile elements point at the
// package documents; an epub may declare more than one. It is never written, so
// unlike the package document a struct is shape enough.
type container struct {
	Rootfiles []*rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  attrURL  `xml:"full-path,attr"`
	MediaType attrText `xml:"media-type,attr"`
}

func newContainer(r io.Reader) (*container, error) {
	var c container
	if err := xml.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// packagePaths returns every package document the container declares, in the
// order they should be tried. Empty means none was declared at all.
//
// The field types carry the normalization and the decoded/raw fallback, so this
// is only the selection rule: which declarations are package documents, and in
// what order to try what each one names.
func (c *container) packagePaths() []string {
	var out []string
	for _, rf := range c.Rootfiles {
		if rf.MediaType != metadataType {
			continue
		}
		out = append(out, rf.FullPath.candidates()...)
	}
	return out
}

// readEncryption parses META-INF/encryption.xml if present. A missing file means
// nothing is encrypted (nil info); a malformed file is reported as an error
// rather than silently treated as "no encryption", since proceeding could
// corrupt a protected entry.
