package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/ocf"
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

// notEpub wraps a malformed-zip error in ErrNotEpub. Other errors, such as a
// missing file or a permission problem, say nothing about the contents and
// pass through unchanged.
func notEpub(path string, err error) error {
	if errors.Is(err, zip.ErrFormat) {
		return fmt.Errorf("%w: %s: %w", ErrNotEpub, path, err)
	}
	return err
}

// archive is the one place entries are looked up by name, so a read and a
// later write always agree on duplicate names, the package document's path,
// and whether an entry exists.
//
// files indexes zr.File without replacing it, since writeTo copies every entry
// in zr.File, duplicates included.
type archive struct {
	zr    *zip.Reader
	files map[string]*zip.File // the first entry of each name
	opf   string
}

// openArchive does not check the mimetype; OpenFile calls validate for that.
func openArchive(zr *zip.Reader) (*archive, error) {
	a := &archive{zr: zr, files: make(map[string]*zip.File, len(zr.File))}
	for _, f := range a.zr.File {
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

func (a *archive) file(name string) *zip.File { return a.files[name] }

func (a *archive) has(name string) bool { return a.files[name] != nil }

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

// validate checks the mimetype entry. calibre only warns about a wrong one,
// but it usually means a zip that is not an epub, such as a .cbz added by
// mistake.
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

// metadataPath returns the package document's path, and guarantees the
// archive holds that entry.
//
// Some Kobo epubs list several rootfiles of which only one exists, so it
// returns the first that does.
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
		// Matched by the entry itself, not only its name. A zip can hold two
		// entries with one name. The replacement was resolved against one of
		// them, and the other must be copied unchanged.
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

	// OCF requires mimetype to be the first entry. Writing it here and skipping
	// it below guarantees that, whatever order the original used.
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

// readEncryption parses META-INF/encryption.xml. It returns nil if the file is
// absent, meaning nothing is encrypted. A malformed file is an error rather
// than "nothing encrypted", since editing a protected entry would corrupt it.
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
