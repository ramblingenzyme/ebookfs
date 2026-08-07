package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

const (
	mimetypePath   = "mimetype"
	mimetypeValue  = "application/epub+zip"
	encryptionPath = "META-INF/encryption.xml"
)

// obfuscationAlgorithms are the two font-obfuscation schemes the EPUB ecosystem
// uses. They appear in encryption.xml exactly like real DRM but are not actually
// encryption — calibre deliberately treats them as readable, and so do we, so a
// book with obfuscated fonts stays editable.
var obfuscationAlgorithms = map[string]bool{
	"http://ns.adobe.com/pdf/enc#RC":     true,
	"http://www.idpf.org/2008/embedding": true,
}

// encryptionInfo records which zip entries are listed in META-INF/encryption.xml
// and under which algorithm, so a real-DRM entry can be distinguished from a
// merely font-obfuscated one (see obfuscationAlgorithms).
type encryptionInfo struct {
	algorithms map[string]string // zip entry name -> EncryptionMethod algorithm
}

// isEncrypted reports whether name is protected by real encryption (as opposed
// to font obfuscation). An entry absent from encryption.xml is not encrypted.
func (e *encryptionInfo) isEncrypted(name string) bool {
	if e == nil {
		return false
	}
	algo, ok := e.algorithms[name]
	return ok && !obfuscationAlgorithms[algo]
}

type encryptionXML struct {
	Data []struct {
		Method struct {
			Algorithm string `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
		Ref struct {
			URI string `xml:"URI,attr"`
		} `xml:"CipherData>CipherReference"`
	} `xml:"EncryptedData"`
}

// readEncryption parses META-INF/encryption.xml if present. A missing file means
// nothing is encrypted (nil info); a malformed file is reported as an error
// rather than silently treated as "no encryption", since proceeding could
// corrupt a protected entry.
func readEncryption(zr *zip.Reader) (*encryptionInfo, error) {
	f := findEntry(zr, encryptionPath)
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var doc encryptionXML
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", encryptionPath, err)
	}

	info := &encryptionInfo{algorithms: make(map[string]string, len(doc.Data))}
	for _, d := range doc.Data {
		if d.Ref.URI != "" && d.Method.Algorithm != "" {
			info.algorithms[d.Ref.URI] = d.Method.Algorithm
		}
	}
	return info, nil
}

func findEntry(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func readEntry(zr *zip.Reader, name string) ([]byte, error) {
	f := findEntry(zr, name)
	if f == nil {
		return nil, fmt.Errorf("entry not found in epub: %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// opfPath resolves the package document's zip-relative path via
// META-INF/container.xml, the same way Parse does on the read side.
func opfPath(zr *zip.Reader) (string, error) {
	container := findEntry(zr, containerPath)
	if container == nil {
		return "", ErrContainer
	}
	// getMetadataPath resolves the package path and, via this predicate, skips
	// Kobo's non-existent rootfile entries and guarantees the returned path is
	// present in the container.
	return getMetadataPath(container, func(name string) bool {
		return findEntry(zr, name) != nil
	})
}

// prepareEpub produces a new epub at a temp file whose entries named in replace
// are swapped for the given bytes and whose every other entry is copied
// verbatim, then re-parses the result to validate it and returns a Commit that
// can apply the change atomically. On any failure the original file is left
// untouched.
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
func prepareEpub(srcPath string, replace map[string][]byte) (*Commit, error) {
	zrc, err := zip.OpenReader(srcPath)
	if err != nil {
		return nil, err
	}
	defer zrc.Close()

	dir := filepath.Dir(srcPath)
	tmp, err := os.CreateTemp(dir, ".ebookfs-*.epub.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	// Explicit cleanup on error paths only; on success the temp is passed to
	// Commit and the caller is responsible for calling Commit or Discard.
	discard := true
	defer func() {
		if discard {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	zw := zip.NewWriter(tmp)
	used := make(map[string]bool, len(replace))

	writeEntry := func(f *zip.File) error {
		if data, ok := replace[f.Name]; ok {
			used[f.Name] = true
			w, err := zw.CreateHeader(&zip.FileHeader{
				Name:     f.Name,
				Method:   f.Method,
				Modified: f.Modified,
			})
			if err != nil {
				return err
			}
			_, err = w.Write(data)
			return err
		}
		if strings.HasSuffix(f.Name, "/") {
			_, err := zw.CreateHeader(&zip.FileHeader{
				Name:     f.Name,
				Method:   f.Method,
				Modified: f.Modified,
			})
			return err
		}
		return zw.Copy(f) // verbatim: raw bytes, original method, no recompression
	}

	// mimetype must come first per the OCF spec; write it before anything else
	// (and skip it in the main loop) regardless of its position in the source.
	if mt := findEntry(&zrc.Reader, mimetypePath); mt != nil {
		if err := writeEntry(mt); err != nil {
			return nil, err
		}
	}
	for _, f := range zrc.File {
		if f.Name == mimetypePath {
			continue
		}
		if err := writeEntry(f); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	for name := range replace {
		if !used[name] {
			return nil, fmt.Errorf("entry not found in epub: %s", name)
		}
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

	discard = false // temp survives — Commit or Discard manages it
	return &Commit{srcPath: srcPath, tmpPath: tmpPath, book: book}, nil
}

// Commit is a prepared epub rewrite that can be applied atomically or discarded.
// A no-op Commit (created when no edits are requested) has Commit and Discard as
// no-ops and Book returns nil.
type Commit struct {
	srcPath string
	tmpPath string
	book    *model.Bib
	noop    bool
}

// Bib returns the reparsed book from the prepared epub, or nil for a no-op
// commit.
func (c *Commit) Bib() *model.Bib { return c.book }

// Commit applies the rewrite by atomically replacing the original with the
// prepared file. For a no-op commit this is a no-op.
func (c *Commit) Commit() error {
	if c.noop {
		return nil
	}
	return os.Rename(c.tmpPath, c.srcPath)
}

// Discard removes the temporary file without touching the original. For a
// no-op commit this is a no-op.
func (c *Commit) Discard() {
	if c.noop {
		return
	}
	os.Remove(c.tmpPath)
}
