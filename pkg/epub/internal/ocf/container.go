package ocf

import (
	"encoding/xml"
	"io"
)

const (
	ContainerPath = "META-INF/container.xml"
	metadataType  = "application/oebps-package+xml"
)

// Container is META-INF/container.xml, which names the package documents. An
// epub may declare more than one.
type Container struct {
	Rootfiles []*rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  AttrURL  `xml:"full-path,attr"`
	MediaType AttrText `xml:"media-type,attr"`
}

func NewContainer(r io.Reader) (*Container, error) {
	var c Container
	if err := xml.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// PackagePaths returns every declared package document path, in the order to
// try them. Whether the archive holds them is the caller's check.
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
