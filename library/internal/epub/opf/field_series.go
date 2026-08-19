package opf

import (
	"strings"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type seriesField struct{ o *Doc }

func (o *Doc) series() seriesField { return seriesField{o} }

// collection returns the EPUB 3 belongs-to-collection recording the series, or
// nil when the series lives in the proprietary calibre metas — or nowhere.
//
// This is the precedence rule, and it is stated once because both directions
// need it: a collection this cannot see is one a write would leave behind, and
// a collection this returns when a read would have fallen through to the calibre
// metas is one a write would carry an empty name over from.
func (f seriesField) collection() *etree.Element {
	for _, m := range f.o.elements("meta") {
		if f.o.isSeriesCollection(m) && text(m) != "" {
			return m
		}
	}
	return nil
}

// get returns the series exactly as the document records it: no index means an
// empty Index, not a default. Defaulting is presentation, and belongs to
// translateSeries, which is building a model for the frontend — set carries the
// index over from here, and must not invent a position the file never had.
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
// standard mechanism, so the proprietary calibre:series metas are used — unlike
// the sort title, series is exposed in the frontend, so a v2 file getting
// nothing would be a silently discarded edit.
//
// Either half is nil when the edit did not name it, and is carried over from
// get — so a rename keeps the book's position and a move keeps the series name,
// taken from the same place a read would have taken them.
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

	// Clearing, or an index edit with no series to move: everything goes, in
	// both encodings.
	if series == "" {
		o.clearCollections(nil)
		o.removeNamedMeta("calibre:series")
		o.removeNamedMeta("calibre:series_index")
		return
	}

	// The EPUB 3 collection is rewritten in place wherever the file has one —
	// whatever the package version claims, since the reader prefers a
	// collection over the calibre metas either way, and a stale one left behind
	// would outrank the edit. A v3 package that has none gets one.
	coll := f.collection()
	if coll == nil && o.epub3() {
		coll = o.metaHome().CreateElement("meta")
		coll.CreateAttr("property", "belongs-to-collection")
	}

	// Only the extra collections go: the one being rewritten stays, with the
	// refinements it carries, since it may hold metadata we did not write —
	// a series identifier, say.
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

	// The proprietary calibre metas are the only encoding EPUB 2 has, so a v2
	// package always gets them; a v3 package gets them only if it already
	// carried them, kept in step rather than left contradicting the collection.
	// Nothing is injected into a file that was not already using the encoding.
	if o.epub3() && len(o.namedMetas("calibre:series")) == 0 {
		return
	}
	o.setNamedMeta("calibre:series", series)
	if position == "" {
		o.removeNamedMeta("calibre:series_index")
		return
	}
	// calibre:series_index is a float by calibre's own convention, so a
	// multi-level D.3.7 position ("2.2.1") is narrowed to its first two levels —
	// the closest thing a calibre reader can act on. The EPUB 3 group-position
	// above keeps it exact.
	o.setNamedMeta("calibre:series_index", calibreIndex(position))
}

// isSeriesCollection reports whether a meta records membership of a series,
// rather than of a set or a publisher bundle. The collection-type must carry no
// scheme: D.3.4 defines series/set "when no scheme is specified", so the same
// word drawn from an external code list means whatever that list says and is
// not ours to read as a series.
//
// The writer uses this too, which is the point — when the two directions had
// separate versions of this test, a collection the reader could not see was
// also invisible to the writer, and a rename added a second one beside it.
func (o *Doc) isSeriesCollection(m *etree.Element) bool {
	return attr(m, "property") == "belongs-to-collection" &&
		o.refine(attr(m, "id"), propCollectionType) == "series"
}

// clearCollections removes every series collection with its refinements, except
// keep. Only series collections go: a set or a publisher bundle is recorded the
// same way, the series field already declines to read one as a series, and
// removing it here would silently discard metadata ebookfs does not own.
//
// Unlike seriesField.collection, this does not require a non-empty name — an
// empty collection is still one of ours to clear.
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

// calibreIndex narrows a series position to what the EPUB 2 encoding can carry:
// calibre:series_index is a float by calibre's convention, so a multi-level
// D.3.7 position keeps only its first two levels.
//
// ponytail: levels past the second are dropped, so 2.2.1 and 2.2.9 both write
// 2.2 into a v2 file. Revisit if a v2 book turns up whose series genuinely
// nests three deep — the EPUB 3 group-position keeps it exact, so only v2
// packages are exposed.
func calibreIndex(s string) string {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 3 {
		return s
	}
	return parts[0] + "." + parts[1]
}
