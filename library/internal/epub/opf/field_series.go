package opf

import (
	"strings"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type seriesField struct{ o *Doc }

func (o *Doc) series() seriesField { return seriesField{o} }

// collection returns the EPUB 3 belongs-to-collection recording the series, or
// nil when the series lives in the calibre metas instead, or nowhere.
func (f seriesField) collection() *etree.Element {
	for _, m := range f.o.elements("meta") {
		if f.o.isSeriesCollection(m) && text(m) != "" {
			return m
		}
	}
	return nil
}

// get returns the series as the document records it: no index means an empty
// Index, not a default.
func (f seriesField) get() *model.SeriesRef {
	if coll := f.collection(); coll != nil {
		return &model.SeriesRef{
			Name:  text(coll),
			Index: f.o.refine(attr(coll, "id"), propGroupPosition),
		}
	}
	name := f.o.namedMeta("calibre:series")
	if name == "" {
		return nil
	}
	return &model.SeriesRef{Name: name, Index: f.o.namedMeta("calibre:series_index")}
}

// set writes the series membership, or clears it when the name is empty. EPUB 3
// records it as a belongs-to-collection meta with refinements; EPUB 2 has no
// standard mechanism, so the proprietary calibre metas are used instead. Either
// half is nil when the edit did not name it, and is carried over from get.
func (f seriesField) set(name, index *string) {
	o := f.o

	series, position := "", ""
	if cur := f.get(); cur != nil {
		series, position = cur.Name, cur.Index
	}
	if name != nil {
		series = collapse(*name)
	}
	if index != nil {
		position = *index
	}

	// Clearing, or an index edit with no series to move.
	if series == "" {
		o.clearCollections(nil)
		o.removeNamedMeta("calibre:series")
		o.removeNamedMeta("calibre:series_index")
		return
	}

	// Rewritten in place wherever the file has one, whatever version it claims,
	// since a stale collection would outrank the calibre metas on the way back
	// in. A v3 package with none gets one.
	coll := f.collection()
	if coll == nil && o.epub3() {
		coll = o.metaHome().CreateElement("meta")
		coll.CreateAttr("property", "belongs-to-collection")
	}

	// Only the extra collections go; this one's refinements may hold metadata we
	// did not write.
	o.clearCollections(coll)

	if coll != nil {
		coll.SetText(series)
		id := o.ensureID(coll, "ebookfs-series")
		o.setRefine(id, propCollectionType, "series", "")
		if position == "" {
			o.removeRefine(id, propGroupPosition)
		} else {
			o.setRefine(id, propGroupPosition, position, "")
		}
	}

	// A v2 package always gets the calibre metas; a v3 package only if it already
	// carried them, kept in step rather than left contradicting the collection.
	if o.epub3() && len(o.namedMetas("calibre:series")) == 0 {
		return
	}
	o.setNamedMeta("calibre:series", series)
	if position == "" {
		o.removeNamedMeta("calibre:series_index")
		return
	}
	o.setNamedMeta("calibre:series_index", calibreIndex(position))
}

// isSeriesCollection reports whether a meta records membership of a series
// rather than of a set or a publisher bundle. The collection-type must carry no
// scheme: D.3.4 defines series and set only "when no scheme is specified".
func (o *Doc) isSeriesCollection(m *etree.Element) bool {
	return attr(m, "property") == "belongs-to-collection" &&
		o.refine(attr(m, "id"), propCollectionType) == "series"
}

// clearCollections removes every series collection and its refinements, except
// keep. Sets and publisher bundles are recorded the same way and are not ours to
// remove. Unlike seriesField.collection an empty name is still ours to clear.
func (o *Doc) clearCollections(keep *etree.Element) {
	var ids []string
	for _, m := range o.elements("meta") {
		if m == keep || !o.isSeriesCollection(m) {
			continue
		}
		ids = append(ids, attr(m, "id"))
		detach(m)
	}
	o.removeMetas(func(m *etree.Element) bool { return refinesAny(m, ids) })
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
