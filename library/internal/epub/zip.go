package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
)

var (
	ErrContainer       = errors.New("no container file found")
	ErrNoRootfile      = errors.New("no package rootfile declared in container")
	ErrRootfileMissing = errors.New("declared package rootfile not found in archive")
	ErrNotEpub         = errors.New("not a valid epub")
)

const (
	containerPath  = "META-INF/container.xml"
	metadataType   = "application/oebps-package+xml"
	mimetypePath   = "mimetype"
	mimetypeValue  = "application/epub+zip"
	encryptionPath = "META-INF/encryption.xml"
)

// archive is the seam between reading an epub and writing one. It owns how an
// entry is located and what the container is checked for, so a read and the
// write that follows it cannot answer those differently — the same reason a
// field in the opf package never touches etree directly.
//
// Every lookup that diverged and had to be fixed lived here: duplicate entries
// resolving last-wins on one side and first-wins on the other, the package
// document's path, the rootfile media-type. They diverged because each caller
// brought its own lookup; the container resolver even took one as a parameter.
//
// files indexes zr.File rather than replacing it. writeTo walks the
// slice in order and copies every entry, duplicates included, so a map alone
// would silently drop entries the rewrite is required to preserve.
//
// The archive does not close anything: Parse drops it at once, Rewrite holds it
// for the copy, and Reader keeps it for the life of the handle.
type archive struct {
	zr    *zip.Reader
	files map[string]*zip.File // index over zr.File; first wins
	opf   string               // package document path, resolved once
}

// openArchive indexes the entries and resolves the package document. It does
// not validate: Parse wants a malformed container reported as ErrNotEpub, and
// callers that only need an entry by name should not pay for the mimetype read.
func openArchive(zr *zip.Reader) (*archive, error) {
	a := &archive{zr: zr, files: make(map[string]*zip.File, len(zr.File))}
	for _, f := range a.zr.File {
		// First wins. A duplicate name is malformed either way; what matters is
		// that every caller resolves it to the same entry.
		if _, dup := a.files[f.Name]; !dup {
			a.files[f.Name] = f
		}
	}

	opf, err := a.metadataPath()
	if err != nil {
		return nil, err
	}
	a.opf = opf
	return a, nil
}

// file returns the entry, or nil. Callers that need the *zip.File rather than
// its bytes read UncompressedSize64 from the central directory without
// decompressing, or Method and Modified when copying.
func (a *archive) file(name string) *zip.File { return a.files[name] }

func (a *archive) has(name string) bool { return a.files[name] != nil }

// size is the entry's uncompressed length from the central directory, so it
// costs no decompression. Absent entries are 0, which is what a caller recording
// a size for something optional wants.
func (a *archive) size(name string) int64 {
	f := a.file(name)
	if f == nil {
		return 0
	}
	return int64(f.UncompressedSize64)
}

func (a *archive) read(name string) ([]byte, error) {
	f := a.file(name)
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

// validate enforces the OCF requirement that the archive declares the epub
// media type. Unlike calibre we reject rather than warn: a wrong mimetype
// usually means a non-epub zip, such as a mis-added .cbz.
func (a *archive) validate() error {
	if !a.has(mimetypePath) {
		return fmt.Errorf("%w: missing mimetype declaration", ErrNotEpub)
	}
	data, err := a.read(mimetypePath)
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(data)); got != mimetypeValue {
		return fmt.Errorf("%w: unexpected mimetype %q", ErrNotEpub, got)
	}
	return nil
}

// metadataPath returns the package document's path from container.xml, and
// guarantees the archive holds an entry under that name — every caller relies on
// that rather than re-checking.
//
// Some Kobo epubs declare several <rootfile> entries where only one exists in
// the zip, so missing ones are skipped and the first that exists is chosen. That
// skipping is why the lookup is the archive's own: a caller supplying its own
// notion of "present" is how the read and write paths came to disagree.
func (a *archive) metadataPath() (string, error) {
	f := a.file(containerPath)
	if f == nil {
		return "", ErrContainer
	}
	r, err := f.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	var c container
	if err := xml.NewDecoder(r).Decode(&c); err != nil {
		return "", err
	}

	var first string
	for _, rf := range c.Rootfiles {
		if opf.Collapse(rf.MediaType) != metadataType {
			continue
		}

		// Decoded first, then the literal. A producer that wrote an unencoded
		// name into both container.xml and the zip has an entry whose name
		// really does contain "%20", so the raw value is the one that matches.
		for _, candidate := range []string{rootfilePath(rf.FullPath), rf.FullPath} {
			if first == "" {
				first = candidate
			}
			if a.has(candidate) {
				return candidate, nil
			}
		}
	}

	if first != "" {
		return "", ErrRootfileMissing
	}

	return "", ErrNoRootfile
}

func (a *archive) writeTo(zw *zip.Writer, replace map[string][]byte) error {
	used := make(map[string]bool, len(replace))
	writeEntry := func(f *zip.File) error {
		// Matched by identity, not by name. A zip may carry two entries under
		// one name; the archive resolved the replacement against exactly one of
		// them, and the other is somebody else's data that this function is
		// contracted to copy verbatim.
		data, ok := replace[f.Name]
		ok = ok && a.file(f.Name) == f

		hdr := &zip.FileHeader{Name: f.Name, Method: f.Method, Modified: f.Modified}
		switch {
		case ok:
			used[f.Name] = true
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			_, err = w.Write(data)
			return err
		case strings.HasSuffix(f.Name, "/"):
			_, err := zw.CreateHeader(hdr)
			return err
		default:
			return zw.Copy(f)
		}
	}

	// mimetype must come first per the OCF spec. Written before anything else and
	// skipped in the main loop, so the guarantee holds for whatever order the
	// source happened to use rather than inheriting it.
	mt := a.file(mimetypePath)
	if mt == nil {
		return fmt.Errorf("%w: missing mimetype declaration", ErrNotEpub)
	}
	if err := writeEntry(mt); err != nil {
		return err
	}

	for _, f := range a.zr.File {
		if f.Name == mimetypePath {
			continue
		}
		if err := writeEntry(f); err != nil {
			return err
		}
	}

	for name := range replace {
		if !used[name] {
			return fmt.Errorf("entry not found in epub: %s", name)
		}
	}

	return nil
}

// rootfilePath decodes a container's full-path. §4.2.6.3.1.3 makes it a
// path-relative-scheme-less-URL string, so a space is written %20 while the zip
// entry it names holds the decoded form.
func rootfilePath(fullPath string) string {
	decoded, err := url.PathUnescape(fullPath)
	if err != nil {
		return fullPath
	}
	return decoded
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
func (a *archive) readEncryption() (*encryptionInfo, error) {
	f := a.file(encryptionPath)
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
			// Keyed by the zip entry name isEncrypted will be asked about.
			// CipherReference/@URI is a URL like the container's full-path, so a
			// space arrives as %20 and the raw value would key the map by a name
			// no entry has — silently reporting an encrypted entry as readable.
			info.algorithms[rootfilePath(d.Ref.URI)] = d.Method.Algorithm
		}
	}
	return info, nil
}

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
