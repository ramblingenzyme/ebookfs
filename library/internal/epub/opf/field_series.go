package opf

import (
	"strings"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type seriesField struct{ o *Doc }

func (o *Doc) series() seriesField { return seriesField{o} }

// get returns the series as the document records it: no index means an empty
// Index, not a default.
func (f seriesField) get() *model.SeriesRef {
	if coll := f.collection(); coll.exists() {
		return &model.SeriesRef{
			Name:  coll.get(),
			Index: coll.refine("group-position").get(),
		}
	}
	// calibre:series and its siblings are <meta name=…> values matched
	// literally (metadata.go), not property names: the colon is part of a
	// proprietary flat string, not a vocabulary prefix, so nothing here goes
	// through spell.
	name := f.o.namedMeta("calibre:series").get()
	if name == "" {
		return nil
	}
	return &model.SeriesRef{Name: name, Index: f.o.namedMeta("calibre:series_index").get()}
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
		series = Collapse(*name)
	}
	if index != nil {
		position = *index
	}

	coll := f.collection()
	calibreName := f.o.namedMeta("calibre:series")
	calibreIdx := f.o.namedMeta("calibre:series_index")

	// Clearing, or an index edit with no series to move.
	if series == "" {
		coll.clear()
		calibreName.clear()
		calibreIdx.clear()
		return
	}

	// Rewritten in place wherever the file has one, whatever version it claims,
	// since a stale collection would outrank the calibre metas on the way back
	// in. A v3 package with none gets one; a v2 package with none stays without
	// one, but still loses any duplicate or empty-named collection.
	if coll.exists() || f.o.epub3() {
		coll.set(series)
		put(coll.refine("group-position"), position)
	} else {
		coll.clear()
	}

	// A v2 package always gets the calibre metas; a v3 package only if it already
	// carried them, kept in step rather than left contradicting the collection.
	if f.o.epub3() && !calibreName.exists() {
		return
	}
	calibreName.set(series)
	put(calibreIdx, calibreIndex(position))
}

// seriesCollection is the belongs-to-collection meta recording the series. It is
// an elementSlot with two extra duties: a write marks the collection as a
// series, and both a write and a clear drop the other series collections, so the
// document is left recording exactly one.
type seriesCollection struct {
	*elementSlot
	f seriesField
}

// collection finds the collection recording the series, or nil when the series
// lives in the calibre metas instead, or nowhere.
func (f seriesField) collection() seriesCollection {
	o := f.o
	var found *etree.Element
	for _, m := range o.elements("meta") {
		if f.isSeriesCollection(m) && text(m) != "" {
			found = m
			break
		}
	}
	return seriesCollection{
		f: f,
		elementSlot: &elementSlot{
			o:        o,
			el:       found,
			idPrefix: "ebookfs-series",
			create: func() *etree.Element {
				m := o.metaParent().CreateElement("meta")
				m.CreateAttr("property", o.spell("belongs-to-collection"))
				return m
			},
		},
	}
}

func (s seriesCollection) set(value string) {
	s.elementSlot.set(value)
	s.markSeries()
	// Only the extra collections go; this one's refinements may hold metadata we
	// did not write.
	s.f.dropCollections(s.el)
}

func (s seriesCollection) clear() {
	s.f.dropCollections(nil)
	s.elementSlot.el = nil
}

// unschemedType returns the collection-type refinement that is ours to read and
// to write. Only an unschemed one is: D.3.4 defines series and set only "when no
// scheme is specified", so a value from someone else's code list is neither.
func (f seriesField) unschemedType(id string) *etree.Element {
	for _, r := range f.o.refineElements(id, "collection-type") {
		if attr(r, "scheme") == "" {
			return r
		}
	}
	return nil
}

// markSeries records the collection as a series rather than a set or a
// publisher bundle.
func (s seriesCollection) markSeries() {
	if m := s.f.unschemedType(s.id()); m != nil {
		m.SetText("series")
		return
	}
	s.refine("collection-type").add("series", "")
}

func (f seriesField) isSeriesCollection(m *etree.Element) bool {
	return f.o.sameProperty(attr(m, "property"), "belongs-to-collection") &&
		text(f.unschemedType(attr(m, "id"))) == "series"
}

// dropCollections removes every series collection except keep, with its
// refinements. Sets and publisher bundles are recorded the same way and are not
// ours to remove. Unlike collection an empty name is still ours to clear.
func (f seriesField) dropCollections(keep *etree.Element) {
	var ids []string
	for _, m := range f.o.elements("meta") {
		if m == keep || !f.isSeriesCollection(m) {
			continue
		}
		ids = append(ids, attr(m, "id"))
		detach(m)
	}
	f.o.removeRefinements(ids)
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
