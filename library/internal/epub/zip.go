package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/ocf"
)

var (
	ErrContainer       = errors.New("no container file found")
	ErrNoRootfile      = errors.New("no package rootfile declared in container")
	ErrRootfileMissing = errors.New("declared package rootfile not found in archive")
	ErrNotEpub         = errors.New("not a valid epub")
)

const (
	mimetypePath  = "mimetype"
	mimetypeValue = "application/epub+zip"
)

// notEpub classifies a failure to open the archive. A malformed zip is not an
// epub; anything else — a missing file, a permission problem, a disk error — is
// the caller's to see verbatim, since it says nothing about the file's contents.
//
// Shared by every entry point, so one broken file is one error however the
// caller got here.
func notEpub(path string, err error) error {
	if errors.Is(err, zip.ErrFormat) {
		return fmt.Errorf("%w: %s: %w", ErrNotEpub, path, err)
	}
	return err
}

// archive owns how an entry is located, so a read and the write that follows it
// cannot resolve a duplicated name, the package document's path, or "present"
// differently.
//
// files indexes zr.File rather than replacing it: writeTo walks the slice in
// order and copies every entry, duplicates included.
//
// Closing is the caller's — Parse drops the archive, Rewrite holds it for the
// copy, Reader for the life of the handle.
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

// validate enforces the OCF mimetype declaration. Unlike calibre we reject
// rather than warn: a wrong mimetype usually means a non-epub zip, such as a
// mis-added .cbz.
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

// metadataPath returns the package document's path, and guarantees the archive
// holds an entry under it — callers rely on that rather than re-checking.
//
// Some Kobo epubs declare several <rootfile> entries where only one exists, so
// missing ones are skipped and the first present one wins.
func (a *archive) metadataPath() (string, error) {
	f := a.file(ocf.ContainerPath)
	if f == nil {
		return "", ErrContainer
	}
	r, err := f.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	c, err := ocf.NewContainer(r)
	if err != nil {
		return "", err
	}

	// container.go decides what the file declares and in what order to try it;
	// only "which of these does this archive actually hold" is the archive's to
	// answer. That is the Kobo case: several rootfiles declared, one present.
	paths := c.PackagePaths()
	if len(paths) == 0 {
		return "", ErrNoRootfile
	}
	for _, p := range paths {
		if a.has(p) {
			return p, nil
		}
	}
	return "", ErrRootfileMissing
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

// readEncryption parses META-INF/encryption.xml if present. A missing file means
// nothing is encrypted (nil info); a malformed file is reported as an error
// rather than silently treated as "no encryption", since proceeding could
// corrupt a protected entry.
func (a *archive) readEncryption() (*ocf.EncryptionInfo, error) {
	f := a.file(ocf.EncryptionPath)
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return ocf.NewEncryptionInfo(rc)
}
