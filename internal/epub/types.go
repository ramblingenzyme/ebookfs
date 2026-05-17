package epub

import (
	"encoding/xml"
	"time"
)

/*
	 --- container.xml ---
		 This is the entry to the epub. It has rootfile elements which point to real .opf files
		 An epub may have multiple .opf files.
*/
type Container struct {
	Rootfiles []*Rootfile `xml:"rootfiles>rootfile"`
}

type Rootfile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}

/*
--- .opf files ---
*/
type opfPackage struct {
	BasePath string
	XMLName  xml.Name          `xml:"package"`
	UniqueId string            `xml:"unique-identifier,attr"`
	Version  string            `xml:"version,attr"`
	Metadata opfMetadata       `xml:"metadata"`
	Manifest []opfManifestItem `xml:"manifest>items"`
}

type opfManifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfMetadata struct {
	Titles      []opfTitle   `xml:"title"`
	Creators    []opfCreator `xml:"creator"`
	Identifiers []Identifier `xml:"identifier"`
	Dates       []string     `xml:"date"`
	Description string       `xml:"description"`
	Metas       []opfMeta    `xml:"meta"`
}

type opfTitle struct {
	Value string `xml:",chardata"`
	ID    string `xml:"id,attr"`
}

type opfCreator struct {
	Name   string `xml:",chardata"`
	Role   string `xml:"role,attr"`    // EPUB 2
	FileAs string `xml:"file-as,attr"` // EPUB 2
	ID     string `xml:"id,attr"`      // for EPUB 3 refines lookup
}

type Identifier struct {
	Value string `xml:",chardata"`
	ID    string `xml:"id,attr"`
}

type opfMeta struct {
	Name     string `xml:"name,attr"`     // EPUB 2
	Content  string `xml:"content,attr"`  // EPUB 2
	Property string `xml:"property,attr"` // EPUB 3 refines
	Refines  string `xml:"refines,attr"`  // EPUB 3 refines
	ID       string `xml:"id,attr"`       // EPUB 3 used to chain refines
	Value    string `xml:",chardata"`     // value for refines
}

type Book struct {
	Title       string
	SortTitle   string
	Description string
	Authors     []Author
	PubDate     time.Time
	CoverPath   string
	Series      string
	SeriesIndex uint16
	Identifiers []Identifier
}

type Author struct {
	Name   string
	SortAs string
}
