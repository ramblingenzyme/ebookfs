package epub

// --- container.xml ---
// This is the entry to the epub. It has rootfile elements which point to real
// .opf files. An epub may have multiple .opf files.
//
// container.xml is read with encoding/xml and never written, so a struct is the
// right shape for it. The package document is different: it is both read and
// written, so it goes through the shared etree model in opf.go instead.
type container struct {
	Rootfiles []*rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}
