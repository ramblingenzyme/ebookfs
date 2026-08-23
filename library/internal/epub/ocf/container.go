package ocf

import (
	"encoding/xml"
	"io"

	epubxml "github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// Reading META-INF/container.xml, which says where the package document is.
// This is only what the container declares; whether the archive holds it is the
// caller's question.

const (
	ContainerPath = "META-INF/container.xml"
	metadataType  = "application/oebps-package+xml"
)

// Container's rootfile elements point at the package documents; an epub may
// declare more than one. Never written, so a struct is shape enough.
type Container struct {
	Rootfiles []*rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  epubxml.AttrURL  `xml:"full-path,attr"`
	MediaType epubxml.AttrText `xml:"media-type,attr"`
}

func NewContainer(r io.Reader) (*Container, error) {
	var c Container
	if err := xml.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// PackagePaths returns every package document declared, in the order to try
// them. Empty means none was declared. The field types carry the normalization
// and the decoded/raw fallback.
func (c *Container) PackagePaths() []string {
	var out []string
	for _, rf := range c.Rootfiles {
		if rf.MediaType != metadataType {
			continue
		}
		out = append(out, rf.FullPath.Candidates()...)
	}
	return out
}
