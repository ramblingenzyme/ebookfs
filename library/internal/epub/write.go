package epub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/content"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/ncx"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/ocf"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Rewrite applies e to the epub at epubPath atomically. Every refusal runs
// before anything is written, so the original survives an error. It returns the
// Bib read back from the file, except when e has no edits at all and b.Bib is
// returned untouched.
//
// An edit asking for what the file already says skips the zip rebuild but is
// still re-parsed, and refusals apply before that is known.
//
// b is used only for validation and to locate the cover entry; its EpubPath is
// not read, so this package never resolves against the store root.
func Rewrite(epubPath string, b *book.Book, e model.Edits) (book.Bib, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return b.Bib, nil
	}

	// Backstop: Library.Edit is the enforcement point, and an unvalidated Edits
	// must never reach a file.
	if v := e.Validate(book.NewImmutableBook(b)); v != nil {
		return book.Bib{}, v
	}

	zrc, err := zip.OpenReader(epubPath)
	if err != nil {
		return book.Bib{}, notEpub(epubPath, err)
	}
	defer zrc.Close()

	a, err := openArchive(&zrc.Reader)
	if err != nil {
		return book.Bib{}, err
	}
	if err := a.validate(); err != nil {
		return book.Bib{}, err
	}

	replace, err := createReplace(a, b, e)
	if err != nil {
		return book.Bib{}, err
	}
	// Nothing to write, but the file is still re-read rather than trusting the
	// Bib the caller handed in: library.Edit builds that from the index, which
	// can disagree with the epub, and an edit is an occasion to reconcile it.
	// Only the zip rebuild is skipped.
	if len(replace) == 0 {
		bib, err := Parse(epubPath)
		if err != nil {
			return book.Bib{}, err
		}
		return *bib, nil
	}

	bib, err := rewriteEpub(epubPath, a, replace)
	if err != nil {
		return book.Bib{}, err
	}
	return *bib, nil
}

func createReplace(a *archive, b *book.Book, e model.Edits) (map[string][]byte, error) {
	// Before any other refusal: it does not depend on which entries the edit
	// turns out to touch. DECISIONS.md #23 says why it is not narrowed to them.
	if a.has(ocf.SignaturesPath) {
		return nil, fmt.Errorf("refusing to edit: the epub is signed (%s) and an edit would invalidate the signature", ocf.SignaturesPath)
	}

	enc, err := a.readEncryption()
	if err != nil {
		return nil, err
	}
	// Parsed whichever edit this is: a cover edit needs it to find the cover page.
	opfBytes, err := a.read(a.opf)
	if err != nil {
		return nil, err
	}
	pkg, err := opf.Parse(opfBytes)
	if err != nil {
		return nil, err
	}

	replace := make(map[string][]byte, 3)

	if e.HasCoverEdit() {
		if err := replaceCover(a, pkg, enc, b, e, replace); err != nil {
			return nil, err
		}
	}

	if e.HasBibEdits() {
		if enc.IsEncrypted(a.opf) {
			return nil, fmt.Errorf("refusing to edit: package document %q is encrypted", a.opf)
		}
		// An edit asking for what the file already says leaves no entry to
		// replace, which is what lets Rewrite skip the rewrite entirely.
		if pkg.Apply(e) {
			newOPF, err := pkg.Bytes()
			if err != nil {
				return nil, err
			}
			replace[a.opf] = newOPF
		}

		if err := replaceNCX(a, pkg, enc, e, replace); err != nil {
			return nil, err
		}
	}

	return replace, nil
}

// replaceCover swaps the cover image entry in place and in the same format, so
// the manifest, the cover-image property and the legacy <meta name="cover">
// keep pointing at what they already did.
func replaceCover(a *archive, pkg *opf.Doc, enc *ocf.EncryptionInfo, b *book.Book, e model.Edits, replace map[string][]byte) error {
	want := coverFormat(b.CoverPath)
	if want == "" {
		return fmt.Errorf("cover format not replaceable in place: %s", b.CoverPath)
	}
	cfg, got, err := image.DecodeConfig(bytes.NewReader(*e.Cover))
	if err != nil {
		return fmt.Errorf("cover data is not a valid PNG or JPEG image: %w", err)
	}
	if got != want {
		return fmt.Errorf("cover image is %s but the epub's cover entry %q is %s; a matching format is required (no transcoding)", got, b.CoverPath, want)
	}
	if !a.has(b.CoverPath) {
		return fmt.Errorf("cover not found in epub: %s", b.CoverPath)
	}
	if enc.IsEncrypted(b.CoverPath) {
		return fmt.Errorf("refusing to replace encrypted cover: %s", b.CoverPath)
	}
	replace[b.CoverPath] = *e.Cover

	return replaceCoverPage(a, pkg, enc, b.CoverPath, cfg.Width, cfg.Height, replace)
}

// replaceCoverPage refits the page displaying the cover; package content says
// why. A candidate from opf is confirmed by finding the cover image inside it,
// so an unreadable one is skipped silently — it was never confirmed to be the
// cover page.
func replaceCoverPage(a *archive, pkg *opf.Doc, enc *ocf.EncryptionInfo, coverPath string, width, height int, replace map[string][]byte) error {
	for _, entry := range pkg.CoverPages(path.Dir(a.opf)) {
		if !a.has(entry) || enc.IsEncrypted(entry) {
			continue
		}
		data, err := a.read(entry)
		if err != nil {
			return err
		}
		doc, err := content.Parse(data, entry)
		if err != nil {
			continue
		}
		if doc.FitCover(coverPath, width, height) {
			out, err := doc.Bytes()
			if err != nil {
				return err
			}
			replace[entry] = out
			return nil
		}
	}
	return nil
}

// coverFormat maps a cover entry's path to the image.DecodeConfig format name
// that may replace it in place, or "" for anything outside calibre's png/jpg/jpeg
// restriction.
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

// rewriteEpub writes a temp epub beside epubPath with the named entries swapped,
// then renames it over the original. The temp file is cleaned up on any failure.
//
// Faithfulness rules, matching what calibre's safe_replace honours:
//   - mimetype is written first and copied byte-for-byte, keeping its STORED
//     form so magic-byte sniffers still recognise the file;
//   - untouched entries are copied raw, preserving order, modtime and method;
//   - every key in replace must match an entry, so a mistargeted edit fails
//     loudly rather than silently dropping.
func rewriteEpub(epubPath string, a *archive, replace map[string][]byte) (*book.Bib, error) {
	dir := filepath.Dir(epubPath)
	tmp, err := os.CreateTemp(dir, ".ebookfs-*.epub.tmp")
	if err != nil {
		return nil, err
	}

	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	zw := zip.NewWriter(tmp)
	if err := a.writeTo(zw, replace); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	// Verify by re-parsing before we touch the original. A blanked title, dropped
	// authors, or any structural breakage fails here and the original survives.
	book, err := Parse(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("rewritten epub failed validation: %w", err)
	}

	return book, os.Rename(tmpPath, epubPath)
}

// replaceNCX adds the rewritten NCX to replace, when the package declares one
// and the edit touches a field it carries; package ncx says why.
//
// An NCX that cannot be read — encrypted, or malformed — is skipped rather than
// failing the edit. The package document is the metadata of record, and
// refusing would leave a book that arrived with an unreadable NCX permanently
// unrenameable.
func replaceNCX(a *archive, pkg *opf.Doc, enc *ocf.EncryptionInfo, e model.Edits, replace map[string][]byte) error {
	if e.Title == nil && e.Authors == nil {
		return nil
	}
	entry := pkg.NCXPath(path.Dir(a.opf))
	if entry == "" || !a.has(entry) || enc.IsEncrypted(entry) {
		return nil
	}

	data, err := a.read(entry)
	if err != nil {
		return err
	}
	// Logged rather than returned: the edit succeeds, and nothing else reports
	// that half of what the book says about itself is now stale.
	doc, err := ncx.Parse(data)
	if err != nil {
		slog.Warn("epub: skipping unreadable NCX; its title and authors will not match the package document",
			"entry", entry, "error", err)
		return nil
	}
	if doc.Apply(e) {
		out, err := doc.Bytes()
		if err != nil {
			return err
		}
		replace[entry] = out
	}
	return nil
}
