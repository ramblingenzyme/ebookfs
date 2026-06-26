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
	"golang.org/x/text/language"
)

const opfNamespace = "http://www.idpf.org/2007/opf"

// Edits is a partial update to a book's bibliographic metadata. A nil pointer
// leaves the field untouched; a non-nil pointer (including one to a zero value)
// applies the change. This lets a caller change exactly one field — e.g. just
// the title — without having to supply the rest.
//
// A non-nil Series pointing at "" removes the series. SeriesIndex is only
// consulted when Series is being set to a non-empty value.
//
// SortTitle is the EPUB 3 file-as refine on the title and follows the same
// nil/empty rules; it is ignored for EPUB 2, which has no standard sort-title
// mechanism. As a special case, changing Title without supplying a SortTitle
// clears any existing sort title — it was derived from the old title, so leaving
// it would make the sort title disagree with the title. (We clear rather than
// regenerate; a heuristic is language-dependent and deferred — see translateTitle.)
type Edits struct {
	Title       *string
	SortTitle   *string
	Description *string
	Language    *string
	Authors     *[]Author
	Series      *string
	SeriesIndex *float64
}

// WriteBib applies edits to the package document of the epub at epubPath and
// rewrites the file in place. The OPF is edited surgically — only the targeted
// <metadata> nodes change, every other zip entry is preserved byte-for-byte —
// and the result is validated by re-parsing before the original is replaced.
// The re-parsed Book is returned so the caller sees exactly what is now on disk.
func WriteBib(epubPath string, e Edits) (*Book, error) {
	zrc, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, err
	}
	opf, err := opfPath(&zrc.Reader)
	if err != nil {
		zrc.Close()
		return nil, err
	}
	enc, err := readEncryption(&zrc.Reader)
	if err != nil {
		zrc.Close()
		return nil, err
	}
	if enc.isEncrypted(opf) {
		zrc.Close()
		return nil, fmt.Errorf("refusing to edit: package document %q is encrypted", opf)
	}
	opfBytes, err := readEntry(&zrc.Reader, opf)
	zrc.Close()
	if err != nil {
		return nil, err
	}

	newOPF, err := editOPF(opfBytes, e)
	if err != nil {
		return nil, err
	}

	return rewriteEpub(epubPath, map[string][]byte{opf: newOPF})
}

// WriteCover replaces the cover image entry (coverPath, as resolved by Parse
// into Book.CoverPath / model.Bib.CoverPath) with img and rewrites the file in
// place. Mirroring calibre's guards, it refuses when the cover entry is
// encrypted and only replaces in-place raster covers (PNG/JPEG); the bytes are
// written verbatim — we do not transcode (no image dependency) — so img must
// already be a valid image whose format matches the existing cover entry.
func WriteCover(epubPath, coverPath string, img []byte) (*Book, error) {
	if coverPath == "" {
		return nil, errors.New("book has no cover to replace")
	}
	want := coverFormat(coverPath)
	if want == "" {
		return nil, fmt.Errorf("cover format not replaceable in place: %s", coverPath)
	}
	// Validate the input is a real image and matches the entry being replaced.
	// Without transcoding, writing e.g. PNG bytes into a cover.jpg entry (whose
	// manifest media-type is image/jpeg) would produce a mismatched epub, and the
	// verify-by-reparse step does not decode the cover to catch it.
	_, got, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return nil, fmt.Errorf("cover data is not a valid PNG or JPEG image: %w", err)
	}
	if got != want {
		return nil, fmt.Errorf("cover image is %s but the epub's cover entry %q is %s; a matching format is required (no transcoding)", got, coverPath, want)
	}

	zrc, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, err
	}
	enc, err := readEncryption(&zrc.Reader)
	if err != nil {
		zrc.Close()
		return nil, err
	}
	exists := findEntry(&zrc.Reader, coverPath) != nil
	encrypted := enc.isEncrypted(coverPath)
	zrc.Close()

	if !exists {
		return nil, fmt.Errorf("cover not found in epub: %s", coverPath)
	}
	if encrypted {
		return nil, fmt.Errorf("refusing to replace encrypted cover: %s", coverPath)
	}

	return rewriteEpub(epubPath, map[string][]byte{coverPath: img})
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
func editOPF(opfBytes []byte, e Edits) ([]byte, error) {
	// Reject a language we can't recognise as a BCP 47 / ISO 639 tag. We validate
	// but deliberately do not normalise: a recognised tag is written through
	// verbatim (so "en" stays "en", not "eng"), unlike calibre which
	// canonicalises. An empty value clears it.
	if e.Language != nil {
		if v := strings.TrimSpace(*e.Language); v != "" {
			if _, err := language.Parse(v); err != nil {
				return nil, fmt.Errorf("language %q is not a recognised BCP 47 / ISO 639 code: %w", *e.Language, err)
			}
		}
	}

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
		// Validated above (unrecognised codes are rejected); written verbatim so a
		// recognised tag is preserved as authored rather than canonicalised the way
		// calibre would (e.g. "en" stays "en", not "eng").
		setDCText(md, dc, "language", *e.Language)
	}
	if e.Authors != nil {
		setAuthors(pkg, md, dc, *e.Authors)
	}
	if e.Series != nil {
		setSeries(pkg, md, *e.Series, e.SeriesIndex)
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
func setAuthors(pkg, md *etree.Element, dc string, authors []Author) {
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
			if a.SortAs != "" {
				addRefine(md, id, "file-as", a.SortAs, "")
			}
		} else {
			opf := ensureOPFPrefix(pkg)
			c.CreateAttr(qualify(opf, "role"), "aut")
			if a.SortAs != "" {
				c.CreateAttr(qualify(opf, "file-as"), a.SortAs)
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
