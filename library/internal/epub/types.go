package epub

import (
	"encoding/xml"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// --- container.xml ---
// This is the entry to the epub. It has rootfile elements which point to real
// .opf files. An epub may have multiple .opf files.
type container struct {
	Rootfiles []*rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}

// --- .opf files ---
type opfPackage struct {
	BasePath string
	XMLName  xml.Name          `xml:"package"`
	Metadata opfMetadata       `xml:"metadata"`
	Manifest []opfManifestItem `xml:"manifest>item"`
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
	Languages   []string     `xml:"language"`
	Dates       []opfDate    `xml:"date"`
	Description string       `xml:"description"`
	Metas       []opfMeta    `xml:"meta"`
}

type opfTitle struct {
	Value string `xml:",chardata"`
	ID    string `xml:"id,attr"`
}

type opfDate struct {
	Value string `xml:",chardata"`
	// Event is the EPUB 2 opf:event ("publication"/"creation"/"modification");
	// matched by local name, so the opf: prefix need not be pinned. Absent in
	// EPUB 3, which carries a single dc:date and stores last-modified elsewhere.
	Event string `xml:"event,attr"`
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
	Authors     []model.Author
	Language    string
	PubDate     string
	CoverPath   string
	Series      string
	SeriesIndex float64
	Identifiers []Identifier
	OpfSize     int64 // uncompressed OPF size from zip central directory; 0 if unavailable
	CoverSize   int64 // uncompressed cover size from zip central directory; 0 if unavailable
	EpubSize    int64 // on-disk epub file size; 0 if unavailable
}
