package pdf

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/atomicfs"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xmp"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xref"
)

// SaveTempPattern names the temporary file Save builds beside the original, in
// os.CreateTemp form. A crash during Save leaves one behind.
const SaveTempPattern = ".pdf-save-*.tmp"

// Save writes the fields that changed since Open. If nothing changed, the file
// is not touched, and no modification date is updated.
//
// Save appends an incremental update holding a new information dictionary and
// XMP packet, and the catalog when it changes. Every field is written to each
// place that disagrees with the Book, not only the fields that changed, since
// the new modification dates decide which place a reader trusts. The update
// sets the information dictionary's ModDate and the packet's ModifyDate and
// MetadataDate to the same instant, as §14.3.4 requires, and gives the file a
// new second ID (§14.4).
//
// The write is atomic. Save builds a SaveTempPattern file next to the
// original, checks that it opens and reads back the Book's metadata, then
// renames it over the original. If any step fails, the original is unchanged.
//
// After Save, the Book reads from the new file, and a later Save compares
// against the values just written.
func (b *Book) Save() error {
	if b.File == nil {
		return errors.New("pdf: Save on a Book that was not opened")
	}

	if !changed(b.Metadata, b.orig) {
		return nil
	}

	u, err := b.stage(time.Now())
	if err != nil {
		return err
	}

	return b.rewrite(u)
}

// stage assembles the update. Every refusal happens before it starts, so a
// refused save allocates no object number.
func (b *Book) stage(now time.Time) (*xref.Update, error) {
	if err := b.refuseSigned(); err != nil {
		return nil, err
	}

	if b.src.packetErr != nil {
		return nil, fmt.Errorf("refusing to edit: rewriting the metadata stream would lose what it holds: %w", b.src.packetErr)
	}

	props, err := b.src.packet.properties(&b.Metadata)
	if err != nil {
		return nil, err
	}

	for _, date := range []string{"ModifyDate", "MetadataDate"} {
		props = append(props, xmp.Property{Name: xmpNS(date), Form: xmp.Simple, Values: []string{formatXMPDate(now)}})
	}

	u := b.x.Update()
	catalog, catalogChanged := b.src.catalog.apply(&b.Metadata)
	info, _ := b.src.info.apply(&b.Metadata)
	info["ModDate"] = cos.TextString(formatPDFDate(now))
	u.Put(u.Trailer, "Info", info)

	packet := &cos.Stream{
		Dict: cos.Dict{"Type": cos.Name("Metadata"), "Subtype": cos.Name("XML")},
		Data: b.src.packet.p.Edit(props),
	}
	if u.Put(catalog, "Metadata", packet) || catalogChanged {
		u.Set(u.Trailer["Root"].(cos.Ref), catalog)
	}

	newSecondID(u.Trailer)

	return u, nil
}

// refuseSigned refuses a signed or certified document (§12.8). An incremental
// update leaves a signature's bytes intact, but validators then report the
// document as changed after signing, and a certification's permissions may
// forbid the change outright.
func (b *Book) refuseSigned() error {
	if _, ok := b.src.catalog.d["Perms"]; ok {
		return errors.New("refusing to edit: the pdf is certified (catalog Perms) and an edit would void it")
	}

	o, err := b.x.Resolve(b.src.catalog.d["AcroForm"])
	if err != nil {
		return err
	}

	form, _ := o.(cos.Dict)
	if flags, _ := cos.Int(form["SigFlags"]); flags&1 != 0 {
		return errors.New("refusing to edit: the pdf is signed (AcroForm SigFlags) and an edit would show it as changed after signing")
	}

	return nil
}

// newSecondID replaces the second file identifier, which §14.4 says changes
// with each revision while the first stays. A file with no ID is left without.
func newSecondID(trailer cos.Dict) {
	if id, ok := trailer["ID"].(cos.Array); ok && len(id) == 2 {
		second := make([]byte, 16)
		_, _ = rand.Read(second) // never fails, as its documentation says

		trailer["ID"] = cos.Array{id[0], cos.String(second)}
	}
}

func (b *Book) rewrite(u *xref.Update) error {
	tmp, err := os.CreateTemp(filepath.Dir(b.path), SaveTempPattern)
	if err != nil {
		return err
	}

	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if err := b.writeUpdated(tmp, u); err != nil {
		return err
	}

	// Opened before the original is touched, so an update that will not read
	// back, or reads back other values, fails here and the original survives.
	next, err := Open(tmpPath)
	if err == nil && changed(next.Metadata, b.Metadata) {
		next.Close()

		err = fmt.Errorf("it reads back %+v, not %+v", next.Metadata, b.Metadata)
	}

	if err != nil {
		return fmt.Errorf("rewritten pdf failed validation: %w", err)
	}

	if err := atomicfs.Rename(tmpPath, b.path); err != nil {
		next.Close()

		return err
	}

	// The open handle follows the file through the rename, so next reads the
	// new file under the original path. b takes the handle over, so it is not
	// closed here.
	next.path = b.path
	b.File.Close()
	*b = *next

	return nil
}

// writeUpdated copies the original byte for byte, mode included, then appends
// the update.
func (b *Book) writeUpdated(tmp *os.File, u *xref.Update) error {
	fi, err := b.f.Stat()
	if err != nil {
		return err
	}

	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		return err
	}

	if _, err := io.Copy(tmp, io.NewSectionReader(b.f, 0, b.size)); err != nil {
		return err
	}

	if err := u.Write(tmp); err != nil {
		return err
	}

	if err := tmp.Sync(); err != nil {
		return err
	}

	return tmp.Close()
}
