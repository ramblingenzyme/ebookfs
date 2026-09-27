package opf

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

const (
	collectionProperty = "belongs-to-collection"
	seriesIDPrefix     = "ebookfs-series"
)

type seriesField struct{ d *pkgdoc.Doc }

func (o *Doc) series() seriesField { return seriesField{o.d} }

// get returns the series as the document records it. No position gives an
// empty Index, not a default.
func (f seriesField) get() *Series {
	if coll := f.collection(); coll.Exists() {
		return &Series{
			Name:  coll.Get(),
			Index: coll.Refine("group-position").Get(),
		}
	}
	// calibre:series is a <meta name>, matched literally rather than as a
	// vocabulary property.
	name := f.d.Named("calibre:series").Get()
	if name == "" {
		return nil
	}
	return &Series{Name: name, Index: f.d.Named("calibre:series_index").Get()}
}

// set writes the series, or clears it when s is nil or has no name. EPUB 3
// uses a belongs-to-collection meta; EPUB 2 has no standard mechanism, so
// calibre's metas are used.
//
// Both halves are written, so a caller changing one reads the other from get.
// An empty-named collection is invisible to get, so the name get reports may
// be the calibre meta's.
func (f seriesField) set(s *Series) {
	series, position := "", ""
	if s != nil {
		series, position = xml.Collapse(s.Name), s.Index
	}

	coll := f.collection()
	calibreName := f.d.Named("calibre:series")
	calibreIdx := f.d.Named("calibre:series_index")

	// Clearing, or an index edit with no series to move.
	if series == "" {
		coll.Clear()
		calibreName.Clear()
		calibreIdx.Clear()
		return
	}

	if writeV3(f.d, coll.Exists()) {
		coll.Set(series)
		pkgdoc.Put(coll.Refine("group-position"), position)
	} else {
		// A v2 package keeps no collection, including a duplicate or
		// empty-named one.
		coll.Clear()
	}

	if writeCalibre(f.d, calibreName.Exists()) {
		calibreName.Set(series)
		pkgdoc.Put(calibreIdx, calibreIndex(position))
	}
}

// seriesCollection is the belongs-to-collection meta holding the series. Its
// Set marks the collection as a series, and its Set and Clear remove every
// other series collection, so the document records exactly one.
//
// The invariant lives here rather than in pkgdoc, which would otherwise need an
// after-write hook for this one field.
type seriesCollection struct {
	*pkgdoc.Element
	f seriesField
}

// collection returns the collection holding the series, or an unbound slot
// when there is none.
func (f seriesField) collection() seriesCollection {
	for _, m := range f.collections() {
		if f.isSeries(m) && m.Get() != "" {
			return seriesCollection{f: f, Element: m}
		}
	}
	return seriesCollection{f: f, Element: f.d.NewPropertyMeta(collectionProperty, seriesIDPrefix)}
}

func (f seriesField) collections() []*pkgdoc.Element {
	return f.d.PropertyMetas(collectionProperty, seriesIDPrefix)
}

func (s seriesCollection) Set(value string) {
	s.Element.Set(value)
	s.markSeries()
	// Only the other collections go; this one's refinements may hold metadata
	// this package did not write.
	s.f.dropCollections(s.Element)
}

func (s seriesCollection) Clear() {
	s.Element.Remove()
	s.f.dropCollections(nil)
}

func (s seriesCollection) markSeries() { s.f.seriesType(s.Element).Set("series") }

// seriesType is the unschemed collection-type refinement. A schemed one is
// someone else's (D.3.4).
func (f seriesField) seriesType(m *pkgdoc.Element) *pkgdoc.Refine {
	return m.Refine("collection-type").Unschemed()
}

func (f seriesField) isSeries(m *pkgdoc.Element) bool { return f.seriesType(m).Get() == "series" }

// dropCollections removes every series collection except keep, with its
// refinements, and leaves sets alone. Unlike collection, it also removes an
// empty-named one.
func (f seriesField) dropCollections(keep *pkgdoc.Element) {
	for _, m := range f.collections() {
		if !m.Same(keep) && f.isSeries(m) {
			m.Remove()
		}
	}
}

// calibreIndex keeps the first two levels of a D.3.7 position, since
// calibre:series_index is a float by calibre's convention.
//
// ponytail: 2.2.1 and 2.2.9 both write 2.2 into a v2 file. The EPUB 3
// group-position keeps it exact. Revisit if a v2 book nests three deep.
func calibreIndex(s string) string {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 3 {
		return s
	}
	return parts[0] + "." + parts[1]
}
