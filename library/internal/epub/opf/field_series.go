package opf

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const (
	collectionProperty = "belongs-to-collection"
	seriesIDPrefix     = "ebookfs-series"
)

type seriesField struct{ d *pkgdoc.Doc }

func (o *Doc) series() seriesField { return seriesField{o.d} }

// get returns the series as the document records it: no index means an empty
// Index, not a default.
func (f seriesField) get() *model.SeriesRef {
	if coll := f.collection(); coll.Exists() {
		return &model.SeriesRef{
			Name:  coll.Get(),
			Index: coll.Refine("group-position").Get(),
		}
	}
	// calibre:series and its siblings are <meta name=…> values matched
	// literally (pkgdoc), not property names: the colon is part of a
	// proprietary flat string, not a vocabulary prefix, so nothing here goes
	// through spell.
	name := f.d.Named("calibre:series").Get()
	if name == "" {
		return nil
	}
	return &model.SeriesRef{Name: name, Index: f.d.Named("calibre:series_index").Get()}
}

// set writes the series membership, or clears it when the name is empty. EPUB 3
// records it as a belongs-to-collection meta with refinements; EPUB 2 has no
// standard mechanism, so the proprietary calibre metas are used instead. Either
// half is nil when the edit did not name it, and is carried over from get.
func (f seriesField) set(name, index *string) {
	series, position := "", ""
	if cur := f.get(); cur != nil {
		series, position = cur.Name, cur.Index
	}
	if name != nil {
		series = xml.Collapse(*name)
	}
	if index != nil {
		position = *index
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

	// Rewritten in place wherever the file has one, whatever version it claims,
	// since a stale collection would outrank the calibre metas on the way back
	// in. A v3 package with none gets one; a v2 package with none stays without
	// one, but still loses any duplicate or empty-named collection.
	if coll.Exists() || f.d.EPUB3() {
		coll.Set(series)
		pkgdoc.Put(coll.Refine("group-position"), position)
	} else {
		coll.Clear()
	}

	// A v2 package always gets the calibre metas; a v3 package only if it already
	// carried them, kept in step rather than left contradicting the collection.
	if f.d.EPUB3() && !calibreName.Exists() {
		return
	}
	calibreName.Set(series)
	pkgdoc.Put(calibreIdx, calibreIndex(position))
}

// seriesCollection is the belongs-to-collection meta recording the series. It is
// a pkgdoc.Element with two extra duties: a write marks the collection as a
// series, and both a write and a clear drop the other series collections, so the
// document is left recording exactly one.
type seriesCollection struct {
	*pkgdoc.Element
	f seriesField
}

// collection finds the collection recording the series, or an unbound slot when
// the series lives in the calibre metas instead, or nowhere.
func (f seriesField) collection() seriesCollection {
	for _, m := range f.collections() {
		if f.isSeries(m) && m.Get() != "" {
			return seriesCollection{f: f, Element: m}
		}
	}
	return seriesCollection{f: f, Element: f.d.NewPropertyMeta(collectionProperty, seriesIDPrefix)}
}

// collections is every collection the document records, series or not.
func (f seriesField) collections() []*pkgdoc.Element {
	return f.d.PropertyMetas(collectionProperty, seriesIDPrefix)
}

func (s seriesCollection) Set(value string) {
	s.Element.Set(value)
	s.markSeries()
	// Only the extra collections go; this one's refinements may hold metadata we
	// did not write.
	s.f.dropCollections(s.Element)
}

func (s seriesCollection) Clear() {
	s.Element.Remove()
	s.f.dropCollections(nil)
}

// markSeries records the collection as a series rather than a set or a
// publisher bundle.
func (s seriesCollection) markSeries() { s.f.seriesType(s.Element).Set("series") }

// seriesType is the collection-type refinement saying which kind a collection
// is. Only an unschemed one is ours to read or to write.
func (f seriesField) seriesType(m *pkgdoc.Element) *pkgdoc.Refine {
	return m.Refine("collection-type").Unschemed()
}

func (f seriesField) isSeries(m *pkgdoc.Element) bool { return f.seriesType(m).Get() == "series" }

// dropCollections removes every series collection except keep, with its
// refinements. Sets and publisher bundles are recorded the same way and are not
// ours to remove. Unlike collection an empty name is still ours to clear.
func (f seriesField) dropCollections(keep *pkgdoc.Element) {
	for _, m := range f.collections() {
		if !m.Same(keep) && f.isSeries(m) {
			m.Remove()
		}
	}
}

// calibreIndex narrows a series position to the float calibre:series_index is by
// calibre's convention, keeping the first two levels of a D.3.7 position.
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
