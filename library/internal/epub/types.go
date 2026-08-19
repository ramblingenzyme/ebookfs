package epub

// container is the entry point to the epub. Its rootfile elements point at the
// package documents; an epub may declare more than one. It is never written, so
// unlike the package document a struct is shape enough.
type container struct {
	Rootfiles []*rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}
