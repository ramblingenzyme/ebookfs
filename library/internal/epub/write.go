package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const opfNamespace = "http://www.idpf.org/2007/opf"

// Prepare creates a temporary epub with the requested changes from e applied
// to the epub at b.EpubPath. Every refusal check runs before the temp file is
// written — the original is never touched on error. The returned Commit can be
// applied atomically via Commit() or discarded via Discard().
func Prepare(b *model.Book, e model.Edits) (*Commit, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return &Commit{noop: true}, nil
	}

	if v := e.Validate(b); v != nil {
		return nil, v
	}

	replace := make(map[string][]byte)

	zrc, err := zip.OpenReader(b.EpubPath)
	if err != nil {
		return nil, err
	}
	defer zrc.Close()

	enc, err := readEncryption(&zrc.Reader)
	if err != nil {
		return nil, err
	}

	if e.HasCoverEdit() {
		want := coverFormat(b.CoverPath)
		if want == "" {
			return nil, fmt.Errorf("cover format not replaceable in place: %s", b.CoverPath)
		}
		_, got, err := image.DecodeConfig(bytes.NewReader(*e.Cover))
		if err != nil {
			return nil, fmt.Errorf("cover data is not a valid PNG or JPEG image: %w", err)
		}
		if got != want {
			return nil, fmt.Errorf("cover image is %s but the epub's cover entry %q is %s; a matching format is required (no transcoding)", got, b.CoverPath, want)
		}
		if findEntry(&zrc.Reader, b.CoverPath) == nil {
			return nil, fmt.Errorf("cover not found in epub: %s", b.CoverPath)
		}
		if enc.isEncrypted(b.CoverPath) {
			return nil, fmt.Errorf("refusing to replace encrypted cover: %s", b.CoverPath)
		}
		replace[b.CoverPath] = *e.Cover
	}

	if e.HasBibEdits() {
		opf, err := opfPath(&zrc.Reader)
		if err != nil {
			return nil, err
		}
		if enc.isEncrypted(opf) {
			return nil, fmt.Errorf("refusing to edit: package document %q is encrypted", opf)
		}
		opfBytes, err := readEntry(&zrc.Reader, opf)
		if err != nil {
			return nil, err
		}
		newOPF, err := editOPF(opfBytes, e)
		if err != nil {
			return nil, err
		}
		replace[opf] = newOPF
	}

	return prepareEpub(b.EpubPath, replace)
}

// writeBib applies edits to the package document of the epub at epubPath,
// rewrites the file in place, and returns the re-parsed Book.
func writeBib(epubPath string, e model.Edits) (*Book, error) {
	c, err := Prepare(&model.Book{Location: model.Location{EpubPath: epubPath}}, e)
	if err != nil {
		return nil, err
	}
	if err := c.Commit(); err != nil {
		c.Discard()
		return nil, err
	}
	return c.Book(), nil
}

// writeCover replaces the cover image entry (coverPath, as resolved by Parse)
// with img, rewrites the file in place, and returns the re-parsed Book.
func writeCover(epubPath, coverPath string, img []byte) (*Book, error) {
	c, err := Prepare(&model.Book{Location: model.Location{EpubPath: epubPath}, Bib: model.Bib{CoverPath: coverPath}}, model.Edits{Cover: &img})
	if err != nil {
		return nil, err
	}
	if err := c.Commit(); err != nil {
		c.Discard()
		return nil, err
	}
	return c.Book(), nil
}

// coverFormat maps a cover entry's path to the image format name (as reported by
// image.DecodeConfig) that may replace it in place, or "" if the extension is
// not an in-place-replaceable raster cover (matching calibre's png/jpg/jpeg
// restriction).
func coverFormat(coverPath string) string {
	switch strings.ToLower(path.Ext(coverPath)) {
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".png":
		return "png"
	default:
		return ""
	}
}

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
		// Written verbatim so a recognised tag is preserved as authored rather than
		// canonicalised the way calibre would (e.g. "en" stays "en", not "eng");
		// validation of the language code is handled by model.Edits.Validate.
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

	for _, m := range md.SelectElements("meta") {
		if titleID != "" && m.SelectAttrValue("property", "") == "file-as" &&
			strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#") == titleID {
			md.RemoveChild(m)
		}
	}

	if strings.TrimSpace(value) == "" {
		return // sort title cleared
	}

	if titleID == "" {
		titleID = "ebookfs-title"
		title.CreateAttr("id", titleID)
	}
	addRefine(md, titleID, "file-as", value, "")
}

// setAuthors replaces the author creators (role "aut", or no role per the EPUB
// default) and their refinements, leaving any non-author creators — editors,
// illustrators — in place. New creators are written in the shape this package's
// own parser reads back: EPUB 3 refines for v3 packages, opf:role/opf:file-as
// attributes for v2.
func setAuthors(pkg, md *etree.Element, dc string, authors []model.Author) {
	epub3 := strings.HasPrefix(packageVersion(pkg), "3")

	var removedIDs []string
	for _, c := range md.SelectElements("creator") {
		if !isAuthorCreator(md, c) {
			continue
		}
		if id := c.SelectAttrValue("id", ""); id != "" {
			removedIDs = append(removedIDs, id)
		}
		md.RemoveChild(c)
	}
	// Drop refinements (role/file-as/etc.) that pointed at the removed creators.
	for _, m := range md.SelectElements("meta") {
		ref := strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#")
		if ref != "" && slices.Contains(removedIDs, ref) {
			md.RemoveChild(m)
		}
	}

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

// setSeries removes any existing series representation (EPUB 3
// belongs-to-collection plus its refines, or the EPUB 2 calibre:series metas)
// and, unless name is empty, writes a fresh one in the version-appropriate shape
// that translateSeries reads back.
//
// Note: unlike the title sort (EPUB 3 only — see setTitleSort), we do write
// calibre's proprietary calibre:series / calibre:series_index metas for EPUB 2.
// Series is exposed for editing in the frontend, EPUB 2 has no standard
// collection model, and those metas are its de-facto convention, so we need that
// fallback. The sort title is not exposed in the frontend, so it needs no EPUB 2
// equivalent and stays EPUB 3 only.
func setSeries(pkg, md *etree.Element, name string, index *float64) {
	var collectionIDs []string
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "belongs-to-collection" {
			if id := m.SelectAttrValue("id", ""); id != "" {
				collectionIDs = append(collectionIDs, id)
			}
			md.RemoveChild(m)
		}
	}
	for _, m := range md.SelectElements("meta") {
		ref := strings.TrimPrefix(m.SelectAttrValue("refines", ""), "#")
		if ref != "" && slices.Contains(collectionIDs, ref) {
			md.RemoveChild(m)
			continue
		}
		switch m.SelectAttrValue("name", "") {
		case "calibre:series", "calibre:series_index":
			md.RemoveChild(m)
		}
	}

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
