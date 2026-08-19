package epub

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const opfNamespace = "http://www.idpf.org/2007/opf"

// editOPF applies edits to OPF bytes using beevik/etree, which round-trips XML
// without rewriting namespace declarations or mangling the dc: prefixes the way
// encoding/xml's encoder would. Untargeted nodes, comments, and formatting are
// left as-is.
func editOPF(opfBytes []byte, e model.Edits) ([]byte, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(opfBytes); err != nil {
		return nil, err
	}
	pkg := doc.SelectElement("package")
	if pkg == nil {
		return nil, errors.New("opf: no <package> element")
	}
	md := pkg.SelectElement("metadata")
	if md == nil {
		return nil, errors.New("opf: no <metadata> element")
	}
	dc := dcPrefix(md)

	setTitle(pkg, md, dc, e.Title, e.SortTitle)

	if e.Description != nil {
		setDCText(md, dc, "description", *e.Description)
	}
	if e.Language != nil {
		setDCText(md, dc, "language", *e.Language)
	}
	if e.Authors != nil {
		setAuthors(pkg, md, dc, *e.Authors)
	}

	if e.Series != nil || e.SeriesIndex != nil {
		setSeries(pkg, md, e.Series, e.SeriesIndex)
	}

	return doc.WriteToBytes()
}

// --- Title, description, language --------------------------------------------

// setTitle writes the title and its sort value, either of which is nil when the
// edit did not name it.
func setTitle(pkg, md *etree.Element, dc string, title, sortTitle *string) {
	if title != nil {
		setDCText(md, dc, "title", *title)
	}

	if sortTitle != nil {
		setTitleSort(pkg, md, *sortTitle)
	} else if title != nil {
		// Clear sort title if title is being updated without new value
		setTitleSort(pkg, md, "")
	}
}

// setTitleSort manages the title's sort value, which in EPUB is the EPUB 3
// file-as refine on the title. EPUB 2 has no standard title-sort mechanism, and
// since the sort title is not exposed for editing in the frontend we do not add
// a proprietary fallback for it (contrast setSeries) — so it is not handled for
// EPUB 2 at all. Removes any existing refine and, unless value is empty, writes a
// fresh one (assigning the title an id if it lacks one).
func setTitleSort(pkg, md *etree.Element, value string) {
	if !isEPUB3(pkg) {
		return // sort titles are an EPUB 3 feature
	}
	title := md.SelectElement("title")
	if title == nil {
		return // no title to sort; title edits are validated elsewhere
	}
	titleID := title.SelectAttrValue("id", "")

	removeRefinements(md, func(m *etree.Element) bool {
		return m.SelectAttrValue("property", "") == "file-as" && refines(m, titleID)
	})

	if strings.TrimSpace(value) == "" {
		return // sort title cleared
	}

	if titleID == "" {
		titleID = "ebookfs-title"
		title.CreateAttr("id", titleID)
	}
	addRefine(md, titleID, "file-as", value, "")
}

// --- Authors -----------------------------------------------------------------

// setAuthors replaces the author creators (role "aut", or no role per the EPUB
// default), leaving any non-author creators — editors, illustrators — in place.
// New creators are written in the shape this package's own parser reads back:
// EPUB 3 refines for v3 packages, opf:role/opf:file-as attributes for v2.
//
// A creator whose name survives the edit is reused rather than rebuilt, so
// refinements this package does not manage (alternate-script, alternate display
// names, third-party metadata) keep pointing at a live creator. Only the role
// and file-as are ours to rewrite. Detaching and re-adding every creator, rather
// than editing them in place, puts them in the order given, which is the order
// translateAuthor reads them back in.
func setAuthors(pkg, md *etree.Element, dc string, authors []model.Author) {
	// The minter reads the ids already in use, so it has to run while the
	// creators are still in the tree.
	nextID := idMinter(md)
	byName, detachedIDs := detachAuthorCreators(md)

	var reusedIDs []string
	for _, a := range authors {
		c, reused := byName[a.Name]
		if reused {
			// Handed out once: a name repeated in the new list gets a fresh
			// creator the second time rather than the same element twice.
			delete(byName, a.Name)
		} else {
			c = etree.NewElement(qualify(dc, "creator"))
		}
		md.AddChild(c)
		c.SetText(a.Name)

		id := c.SelectAttrValue("id", "")
		if reused && id != "" {
			reusedIDs = append(reusedIDs, id)
			clearManagedRefines(md, id, "role", "file-as")
		} else {
			id = nextID()
			c.CreateAttr("id", id)
		}
		writeAuthorMetadata(pkg, md, c, id, a)
	}

	// Refinements follow their creator: a reused one keeps everything we did not
	// rewrite, and a creator that never made it back loses them all.
	removeRefinements(md, func(m *etree.Element) bool {
		return refinesAny(m, detachedIDs) && !refinesAny(m, reusedIDs)
	})
}

// detachAuthorCreators removes every author creator from the metadata and
// returns them keyed by name, plus the ids of all of them. The name is how an
// author is matched to the element it already had, so a creator without one —
// or a second creator sharing a name — is detached but left unmatchable. The
// ids are what the caller sweeps refinements against.
func detachAuthorCreators(md *etree.Element) (map[string]*etree.Element, []string) {
	byName := map[string]*etree.Element{}
	var ids []string
	for _, c := range md.SelectElements("creator") {
		if !isAuthorCreator(md, c) {
			continue
		}
		md.RemoveChild(c)
		ids = append(ids, c.SelectAttrValue("id", ""))
		name := strings.TrimSpace(c.Text())
		if _, seen := byName[name]; name != "" && !seen {
			byName[name] = c
		}
	}
	return byName, ids
}

// idMinter hands out creator ids that are not already in use. A reused creator
// keeps the id it came with, which may well be one we minted on an earlier
// write, so counting from the loop index would eventually collide and leave two
// creators answering to the same refinements.
func idMinter(md *etree.Element) func() string {
	taken := map[string]bool{}
	for _, c := range md.SelectElements("creator") {
		if id := c.SelectAttrValue("id", ""); id != "" {
			taken[id] = true
		}
	}
	n := 0
	return func() string {
		for {
			n++
			id := fmt.Sprintf("ebookfs-creator-%d", n)
			if !taken[id] {
				taken[id] = true
				return id
			}
		}
	}
}

// writeAuthorMetadata records the role and sort name in the shape this
// package's own parser reads back: refinements for EPUB 3, opf: attributes for
// EPUB 2. The file-as attribute is cleared first, since the author may have
// lost the sort name it was written with.
func writeAuthorMetadata(pkg, md, c *etree.Element, id string, a model.Author) {
	if isEPUB3(pkg) {
		addRefine(md, id, "role", "aut", "marc:relators")
		if a.SortName != "" {
			addRefine(md, id, "file-as", a.SortName, "")
		}
		return
	}
	opf := ensureOPFPrefix(pkg)
	c.CreateAttr(qualify(opf, "role"), "aut")
	c.RemoveAttr(qualify(opf, "file-as"))
	if a.SortName != "" {
		c.CreateAttr(qualify(opf, "file-as"), a.SortName)
	}
}

// isAuthorCreator reports whether a creator element is one setAuthors owns,
// resolving its role from the EPUB 2 attribute or the EPUB 3 refine.
func isAuthorCreator(md, c *etree.Element) bool {
	role := c.SelectAttrValue("role", "")
	if role == "" {
		role = refineValue(md, c.SelectAttrValue("id", ""), "role")
	}
	return isAuthorRole(role)
}

// --- Series ------------------------------------------------------------------

// setSeries replaces the series with name and index, or clears it if name is
// empty. EPUB 3 uses belongs-to-collection; EPUB 2 uses calibre:series. Unlike
// setTitleSort, we write calibre:series for EPUB 2 because series is exposed in
// the frontend.
//
// Name and index are nil when the edit did not name them. Since the series is
// rewritten wholesale, whichever half the edit left out is carried over from the
// file: a rename keeps the book's position, and a move keeps the series name.
func setSeries(pkg, md *etree.Element, name *string, index *float64) {
	// Read before clearSeries, which is about to take both halves with it.
	curName, curIndex := currentSeries(md)
	if name == nil {
		name = &curName
	}
	if index == nil {
		index = curIndex
	}

	series := strings.TrimSpace(*name)
	epub3 := isEPUB3(pkg)

	// A collection element can carry refinements we did not write — a series
	// identifier, say — so it is rewritten in place rather than replaced, the
	// way a creator is. Nothing refines the EPUB 2 calibre metas, so they have
	// nothing to lose and are simply written fresh.
	var coll *etree.Element
	if series != "" && epub3 {
		coll = seriesCollection(md)
	}
	clearSeries(md, coll)

	if series == "" {
		return // series cleared, or an index edit with no series to move
	}

	if !epub3 {
		newNamedMeta(md, "calibre:series", *name)
		if index != nil {
			newNamedMeta(md, "calibre:series_index", formatIndex(*index))
		}
		return
	}

	if coll == nil {
		coll = md.CreateElement("meta")
		coll.CreateAttr("property", "belongs-to-collection")
	}
	id := coll.SelectAttrValue("id", "")
	if id == "" {
		id = "ebookfs-series"
		coll.CreateAttr("id", id)
	}
	coll.SetText(*name)
	clearManagedRefines(md, id, "collection-type", "group-position")
	addRefine(md, id, "collection-type", "series", "")
	if index != nil {
		addRefine(md, id, "group-position", formatIndex(*index), "")
	}
}

// seriesCollection returns the meta recording the book's series membership, or
// nil if the file records none in EPUB 3 form. A file with several keeps the
// first, which is the one currentSeries reads.
func seriesCollection(md *etree.Element) *etree.Element {
	for _, m := range md.SelectElements("meta") {
		if isSeriesCollection(md, m) {
			return m
		}
	}
	return nil
}

// clearSeries removes the series in both encodings: the EPUB 3
// belongs-to-collection with its refinements, and the EPUB 2 calibre metas.
// Only series collections go. A set or a publisher bundle is recorded the same
// way and translateSeries already declines to read one as a series, so removing
// it here would silently discard metadata ebookfs does not own.
//
// The keep element, when non-nil, stays with its refinements: the caller is
// about to rewrite it and clears the ones it owns itself.
func clearSeries(md, keep *etree.Element) {
	var ids []string
	for _, m := range md.SelectElements("meta") {
		if m == keep || !isSeriesCollection(md, m) {
			continue
		}
		ids = append(ids, m.SelectAttrValue("id", ""))
		md.RemoveChild(m)
	}
	removeRefinements(md, func(m *etree.Element) bool {
		switch m.SelectAttrValue("name", "") {
		case "calibre:series", "calibre:series_index":
			return true
		}
		return refinesAny(m, ids)
	})
}

// currentSeries reads the series already recorded in the OPF, mirroring
// translateSeries' precedence: an EPUB 3 belongs-to-collection wins, falling
// back to the EPUB 2 calibre:series metas. A nil index means the file records
// no position; unlike translateSeries this does not default it to 1, because
// the caller is deciding whether to write one at all.
func currentSeries(md *etree.Element) (string, *float64) {
	if coll := seriesCollection(md); coll != nil {
		pos := refineValue(md, coll.SelectAttrValue("id", ""), "group-position")
		return strings.TrimSpace(coll.Text()), parseIndex(pos)
	}
	var (
		name string
		idx  *float64
	)
	for _, m := range md.SelectElements("meta") {
		switch m.SelectAttrValue("name", "") {
		case "calibre:series":
			name = strings.TrimSpace(m.SelectAttrValue("content", ""))
		case "calibre:series_index":
			idx = parseIndex(m.SelectAttrValue("content", ""))
		}
	}
	return name, idx
}

// isSeriesCollection reports whether a meta is a collection membership, and one
// that is a series rather than a set or a bundle, by the same collection-type
// rule translateSeries reads with.
func isSeriesCollection(md, m *etree.Element) bool {
	return m.SelectAttrValue("property", "") == "belongs-to-collection" &&
		refineValue(md, m.SelectAttrValue("id", ""), "collection-type") == "series"
}

// formatIndex writes a series position in its shortest exact form: 3 -> "3",
// 2.5 -> "2.5". It round-trips through parseIndex.
func formatIndex(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func parseIndex(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

// --- OPF and etree plumbing --------------------------------------------------

// isEPUB3 reports whether the package is EPUB 3, which decides how metadata is
// written: refinements for v3, opf: attributes and calibre metas for v2. The
// version attribute is "3.0"/"3.1"/"2.0", so the major digit is the test.
func isEPUB3(pkg *etree.Element) bool {
	return strings.HasPrefix(pkg.SelectAttrValue("version", ""), "3")
}

// setDCText sets the text of the first dc element with the given local name,
// creating it (with the document's dc prefix) if absent. Matching by local name
// is prefix-agnostic, so it finds the element whatever prefix the source uses.
func setDCText(md *etree.Element, dc, tag, value string) {
	if el := md.SelectElement(tag); el != nil {
		el.SetText(value)
		return
	}
	md.CreateElement(qualify(dc, tag)).SetText(value)
}

// clearManagedRefines removes the named refinements from the element with the
// given id, so reusing it does not leave it with two of each once they are
// written again. Anything refining it under another property belongs to
// whoever put it there and stays.
func clearManagedRefines(md *etree.Element, id string, properties ...string) {
	removeRefinements(md, func(m *etree.Element) bool {
		return refines(m, id) && slices.Contains(properties, m.SelectAttrValue("property", ""))
	})
}

// dcPrefix returns the namespace prefix the document uses for Dublin Core
// elements, inferred from the always-present <dc:title>, so any new dc element
// we create matches the existing declaration. Defaults to "dc".
func dcPrefix(md *etree.Element) string {
	if t := md.SelectElement("title"); t != nil {
		return t.Space
	}
	return "dc"
}

func qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}

// ensureOPFPrefix returns the prefix bound to the OPF namespace, declaring
// xmlns:opf if the document only has it as the default namespace (attributes
// cannot use a default namespace, so prefixed opf:role/opf:file-as need one).
func ensureOPFPrefix(pkg *etree.Element) string {
	for i := range pkg.Attr {
		a := pkg.Attr[i]
		if a.Space == "xmlns" && a.Value == opfNamespace {
			return a.Key
		}
	}
	pkg.CreateAttr("xmlns:opf", opfNamespace)
	return "opf"
}

func newNamedMeta(md *etree.Element, name, content string) {
	m := md.CreateElement("meta")
	m.CreateAttr("name", name)
	m.CreateAttr("content", content)
}

// addRefine appends an EPUB 3 refinement <meta> targeting the element with the
// given id.
func addRefine(md *etree.Element, id, property, value, scheme string) {
	m := md.CreateElement("meta")
	m.CreateAttr("refines", "#"+id)
	m.CreateAttr("property", property)
	if scheme != "" {
		m.CreateAttr("scheme", scheme)
	}
	m.SetText(value)
}

func refineValue(md *etree.Element, id, property string) string {
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == property && refines(m, id) {
			return m.Text()
		}
	}
	return ""
}

// refines reports whether m is a refinement targeting the element with the
// given id. An empty id matches nothing: every meta without a refines attribute
// would otherwise compare equal to it.
func refines(m *etree.Element, id string) bool {
	return id != "" && strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#") == id
}

// refinesAny reports whether m refines any of the given ids.
func refinesAny(m *etree.Element, ids []string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return refines(m, id) })
}

// removeRefinements removes meta elements matching the predicate.
func removeRefinements(md *etree.Element, predicate func(*etree.Element) bool) {
	for _, m := range md.SelectElements("meta") {
		if predicate(m) {
			md.RemoveChild(m)
		}
	}
}
