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

	if e.Title != nil {
		setDCText(md, dc, "title", *e.Title)
	}
	// An explicit sort title wins; otherwise a title change invalidates any
	// existing sort title, so clear it.
	if e.SortTitle != nil {
		setTitleSort(pkg, md, *e.SortTitle)
	} else if e.Title != nil {
		setTitleSort(pkg, md, "")
	}

	if e.Description != nil {
		setDCText(md, dc, "description", *e.Description)
	}
	if e.Language != nil {
		setDCText(md, dc, "language", *e.Language)
	}
	if e.Authors != nil {
		setAuthors(pkg, md, dc, *e.Authors)
	}

	if e.Series != nil {
		setSeries(pkg, md, *e.Series, e.SeriesIndex)
	} else if e.SeriesIndex != nil {
		// Index-only update: preserve the current series name from the OPF
		// and rewrite it with the new position.
		if name := currentSeriesName(md); name != "" {
			setSeries(pkg, md, name, e.SeriesIndex)
		}
	}

	return doc.WriteToBytes()
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

// setTitleSort manages the title's sort value, which in EPUB is the EPUB 3
// file-as refine on the title. EPUB 2 has no standard title-sort mechanism, and
// since the sort title is not exposed for editing in the frontend we do not add
// a proprietary fallback for it (contrast setSeries) — so it is not handled for
// EPUB 2 at all. Removes any existing refine and, unless value is empty, writes a
// fresh one (assigning the title an id if it lacks one).
func setTitleSort(pkg, md *etree.Element, value string) {
	if !strings.HasPrefix(packageVersion(pkg), "3") {
		return // sort titles are an EPUB 3 feature
	}
	title := md.SelectElement("title")
	if title == nil {
		return // no title to sort; title edits are validated elsewhere
	}
	titleID := title.SelectAttrValue("id", "")

	removeRefinements(md, func(m *etree.Element) bool {
		return titleID != "" && m.SelectAttrValue("property", "") == "file-as" &&
			strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#") == titleID
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

// removeElements removes elements matching selector and match, returning their IDs.
func removeElements(md *etree.Element, selector string, match func(*etree.Element) bool) []string {
	var removedIDs []string
	for _, el := range md.SelectElements(selector) {
		if !match(el) {
			continue
		}
		if id := el.SelectAttrValue("id", ""); id != "" {
			removedIDs = append(removedIDs, id)
		}
		md.RemoveChild(el)
	}
	return removedIDs
}

// removeRefinements removes meta elements matching the predicate.
func removeRefinements(md *etree.Element, predicate func(*etree.Element) bool) {
	for _, m := range md.SelectElements("meta") {
		if predicate(m) {
			md.RemoveChild(m)
		}
	}
}

// setAuthors replaces the author creators (role "aut", or no role per the EPUB
// default) and their refinements, leaving any non-author creators — editors,
// illustrators — in place. New creators are written in the shape this package's
// own parser reads back: EPUB 3 refines for v3 packages, opf:role/opf:file-as
// attributes for v2.
func setAuthors(pkg, md *etree.Element, dc string, authors []model.Author) {
	epub3 := strings.HasPrefix(packageVersion(pkg), "3")

	removedIDs := removeElements(md, "creator", func(c *etree.Element) bool {
		return isAuthorCreator(md, c)
	})
	removeRefinements(md, func(m *etree.Element) bool {
		ref := strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#")
		return ref != "" && slices.Contains(removedIDs, ref)
	})

	for i, a := range authors {
		id := fmt.Sprintf("ebookfs-creator-%d", i+1)
		c := md.CreateElement(qualify(dc, "creator"))
		c.CreateAttr("id", id)
		c.SetText(a.Name)
		if epub3 {
			addRefine(md, id, "role", "aut", "marc:relators")
			if a.SortName != "" {
				addRefine(md, id, "file-as", a.SortName, "")
			}
		} else {
			opf := ensureOPFPrefix(pkg)
			c.CreateAttr(qualify(opf, "role"), "aut")
			if a.SortName != "" {
				c.CreateAttr(qualify(opf, "file-as"), a.SortName)
			}
		}
	}
}

// isAuthorCreator mirrors translateAuthor: a creator counts as an author when
// its effective role is "aut" or unspecified.
func isAuthorCreator(md, c *etree.Element) bool {
	role := c.SelectAttrValue("role", "")
	if role == "" {
		role = refineValue(md, c.SelectAttrValue("id", ""), "role")
	}
	return role == "" || role == "aut"
}

// setSeries replaces the existing series with name and index, or clears it if
// name is empty. EPUB 3 uses belongs-to-collection; EPUB 2 uses calibre:series.
// Unlike setTitleSort, we write calibre:series for EPUB 2 because series is
// exposed in the frontend.
func setSeries(pkg, md *etree.Element, name string, index *float64) {
	collectionIDs := removeElements(md, "meta", func(m *etree.Element) bool {
		return m.SelectAttrValue("property", "") == "belongs-to-collection"
	})
	removeRefinements(md, func(m *etree.Element) bool {
		ref := strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#")
		if ref != "" && slices.Contains(collectionIDs, ref) {
			return true
		}
		switch m.SelectAttrValue("name", "") {
		case "calibre:series", "calibre:series_index":
			return true
		}
		return false
	})

	if strings.TrimSpace(name) == "" {
		return // series cleared
	}

	var idx string
	if index != nil {
		// Shortest exact form: 3 -> "3", 2.5 -> "2.5"; round-trips via ParseFloat.
		idx = strconv.FormatFloat(*index, 'g', -1, 64)
	}

	if strings.HasPrefix(packageVersion(pkg), "3") {
		const id = "ebookfs-series"
		m := md.CreateElement("meta")
		m.CreateAttr("property", "belongs-to-collection")
		m.CreateAttr("id", id)
		m.SetText(name)
		addRefine(md, id, "collection-type", "series", "")
		if idx != "" {
			addRefine(md, id, "group-position", idx, "")
		}
	} else {
		newNamedMeta(md, "calibre:series", name)
		if idx != "" {
			newNamedMeta(md, "calibre:series_index", idx)
		}
	}
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
	if id == "" {
		return ""
	}
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == property &&
			strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#") == id {
			return m.Text()
		}
	}
	return ""
}

// currentSeriesName reads the existing series name from the OPF metadata,
// preferring EPUB 3 belongs-to-collection over EPUB 2 calibre:series.
func currentSeriesName(md *etree.Element) string {
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "belongs-to-collection" &&
			refineValue(md, m.SelectAttrValue("id", ""), "collection-type") == "series" {
			return strings.TrimSpace(m.Text())
		}
	}
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("name", "") == "calibre:series" {
			return strings.TrimSpace(m.SelectAttrValue("content", ""))
		}
	}
	return ""
}

func packageVersion(pkg *etree.Element) string {
	return pkg.SelectAttrValue("version", "")
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
