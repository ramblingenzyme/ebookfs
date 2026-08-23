package epub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Rewrite applies the changes in e to the epub at epubPath atomically. Every
// refusal check runs before any file is written, so the original is untouched
// on error. It returns the book's authoritative Bib, read from the epub, except
// on the one path that never opens it: an e with no bib or cover edits returns
// b.Bib untouched.
//
// A bib edit asking for what the package document already says skips the zip
// rebuild but is still re-parsed, so the answer comes from the file either way.
// Refusals apply before that is known: an edit is checked against the epub
// before it is found to be a no-op.
//
// b is used only for validation and for locating the cover entry; its EpubPath
// is not read, so this package never resolves a path against the store root.
func Rewrite(epubPath string, b *model.Book, e model.Edits) (model.Bib, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return b.Bib, nil
	}

	// Backstop: Library.Edit is the enforcement point, and an unvalidated Edits
	// must never reach a file.
	if v := e.Validate(b); v != nil {
		return model.Bib{}, v
	}

	zrc, err := zip.OpenReader(epubPath)
	if err != nil {
		return model.Bib{}, notEpub(epubPath, err)
	}
	defer zrc.Close()

	a, err := openArchive(&zrc.Reader)
	if err != nil {
		return model.Bib{}, err
	}
	if err := a.validate(); err != nil {
		return model.Bib{}, err
	}

	replace, err := createReplace(a, b, e)
	if err != nil {
		return model.Bib{}, err
	}
	// Nothing to write, but the file is still re-read rather than trusting the
	// Bib the caller handed in: library.Edit builds that from the index, which
	// can disagree with the epub, and an edit is an occasion to reconcile it.
	// Only the zip rebuild is skipped.
	if len(replace) == 0 {
		bib, err := Parse(epubPath)
		if err != nil {
			return model.Bib{}, err
		}
		return *bib, nil
	}

	bib, err := rewriteEpub(epubPath, a, replace)
	if err != nil {
		return model.Bib{}, err
	}
	return *bib, nil
}

func createReplace(a *archive, b *model.Book, e model.Edits) (map[string][]byte, error) {
	enc, err := a.readEncryption()
	if err != nil {
		return nil, err
	}
	replace := make(map[string][]byte, 2)

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
		if !a.has(b.CoverPath) {
			return nil, fmt.Errorf("cover not found in epub: %s", b.CoverPath)
		}
		if enc.isEncrypted(b.CoverPath) {
			return nil, fmt.Errorf("refusing to replace encrypted cover: %s", b.CoverPath)
		}
		replace[b.CoverPath] = *e.Cover
	}

	if e.HasBibEdits() {
		opfEntry := a.opf
		if enc.isEncrypted(opfEntry) {
			return nil, fmt.Errorf("refusing to edit: package document %q is encrypted", opfEntry)
		}
		opfBytes, err := a.read(opfEntry)
		if err != nil {
			return nil, err
		}
		doc, err := opf.Parse(opfBytes)
		if err != nil {
			return nil, err
		}
		// An edit asking for what the file already says leaves no entry to
		// replace, which is what lets Rewrite skip the rewrite entirely.
		if doc.Apply(e) {
			newOPF, err := doc.Bytes()
			if err != nil {
				return nil, err
			}
			replace[opfEntry] = newOPF
		}
	}

	return replace, nil
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

// rewriteEpub creates a temporary epub in the same directory as epubPath whose
// entries named in replace are swapped for the given bytes and whose every
// other entry is copied verbatim, then atomically replaces the original.
// Returns the parsed Bib on success. On any failure the temp file is cleaned up.
//
// Faithfulness rules (matching the OCF container requirements calibre's
// safe_replace also honours):
//   - the "mimetype" entry is written first and copied byte-for-byte, preserving
//     its STORED (uncompressed, no-extra-field) form so magic-byte sniffers keep
//     recognising the file;
//   - all untouched entries are copied raw (no recompression), preserving order,
//     modtime, and method;
//   - every key in replace must match an existing entry, so a mistargeted edit
//     fails loudly instead of silently dropping.
func rewriteEpub(epubPath string, a *archive, replace map[string][]byte) (*model.Bib, error) {
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
