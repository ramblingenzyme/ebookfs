package pdf_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/pdftest"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf"
)

var richDoc = pdftest.Doc{
	Info: map[string]string{"Title": "(Old Title)", "Author": "(Old Author)", "Producer": "(Some Producer)"},
	XMP: pdftest.Packet(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Old Title</rdf:li></rdf:Alt></dc:title>
<dc:creator><rdf:Seq><rdf:li>Old Author</rdf:li></rdf:Seq></dc:creator>
<x:custom xmlns:x="urn:example:unknown">kept</x:custom>`),
}

func TestSaveRoundTripsEveryMetadataField(t *testing.T) {
	for name, doc := range map[string]pdftest.Doc{"with XMP": richDoc, "without XMP": {}} {
		t.Run(name, func(t *testing.T) {
			eachLayout(t, doc, func(t *testing.T, path string) {
				var want pdf.Metadata
				fillMetadata(t, &want)

				mustSave(t, path, func(b *pdf.Book) { b.Metadata = want })

				if got := open(t, path).Metadata; !reflect.DeepEqual(got, want) {
					t.Fatalf("after setting every field:\n got %+v\nwant %+v", got, want)
				}

				// take must clone a slice, or the in-place change also lands
				// in orig and Save sees nothing to write.
				mutated := want
				fillMetadata(t, &mutated)
				mutateSlices(&mutated)

				mustSave(t, path, func(b *pdf.Book) { mutateSlices(&b.Metadata) })

				if got := open(t, path).Metadata; !reflect.DeepEqual(got, mutated) {
					t.Fatalf("after changing slices in place:\n got %+v\nwant %+v", got, mutated)
				}

				mustSave(t, path, func(b *pdf.Book) { b.Metadata = pdf.Metadata{} })

				if got := open(t, path).Metadata; !equal(got, pdf.Metadata{}) {
					t.Errorf("after clearing every field: %+v", got)
				}
			})
		})
	}
}

func TestSaveWithoutAChangeLeavesTheFileUntouched(t *testing.T) {
	eachLayout(t, richDoc, func(t *testing.T, path string) {
		before := read(t, path)

		mustSave(t, path, func(b *pdf.Book) { b.Title += "" })

		if !bytes.Equal(before, read(t, path)) {
			t.Error("Save with nothing changed rewrote the file")
		}
	})
}

// §7.5.6: an incremental update leaves the original bytes intact.
func TestSaveAppendsToTheOriginalBytes(t *testing.T) {
	eachLayout(t, richDoc, func(t *testing.T, path string) {
		before := read(t, path)

		mustSave(t, path, func(b *pdf.Book) { b.Title = "New" })

		after := read(t, path)
		if !bytes.HasPrefix(after, before) {
			t.Fatal("the saved file does not begin with the original")
		}

		if update := after[len(before):]; !bytes.HasSuffix(update, []byte("%%EOF\n")) {
			t.Errorf("the update does not end with %%%%EOF: %q", update[max(0, len(update)-40):])
		}
	})
}

// Everything the packet holds that Save does not own survives, and so does a
// language alternative while the x-default it sits beside is unchanged.
func TestSavePreservesWhatItDoesNotOwn(t *testing.T) {
	doc := richDoc
	doc.XMP = strings.Replace(doc.XMP, `<rdf:li xml:lang="x-default">Old Title</rdf:li>`,
		`<rdf:li xml:lang="x-default">Old Title</rdf:li><rdf:li xml:lang="de">Alter Titel</rdf:li>`, 1)

	eachLayout(t, doc, func(t *testing.T, path string) {
		mustSave(t, path, func(b *pdf.Book) { b.Publisher = "New Publisher" })

		out := read(t, path)
		for _, want := range []string{"urn:example:unknown", ">kept<", "Alter Titel", "(Some Producer)"} {
			if !bytes.Contains(out[len(pdftest.Bytes(doc)):], []byte(want)) {
				t.Errorf("the update lost %q", want)
			}
		}
	})
}

// Once one field changes, the modification dates decide which store a reader
// trusts, so a stale one must be brought up to date with the rest.
func TestSaveBringsAStaleStoreUpToDate(t *testing.T) {
	doc := pdftest.Doc{
		Info: map[string]string{"Title": "(Newer Title)", "ModDate": "(D:20240101000000Z)"},
		XMP: pdftest.Packet(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Stale Title</rdf:li></rdf:Alt></dc:title>
<xmp:MetadataDate>2020-01-01T00:00:00Z</xmp:MetadataDate>`),
	}

	eachLayout(t, doc, func(t *testing.T, path string) {
		if got := open(t, path).Title; got != "Newer Title" {
			t.Fatalf("before saving, Title = %q", got)
		}

		mustSave(t, path, func(b *pdf.Book) { b.Publisher = "Pub" })

		if got := open(t, path).Title; got != "Newer Title" {
			t.Errorf("after an unrelated edit, Title = %q, want the newer one kept", got)
		}

		if out := read(t, path); bytes.Count(out, []byte("Stale Title")) != 1 {
			t.Error("the stale XMP title was not replaced")
		}
	})
}

// The dictionary's Author is one string, so several authors are joined there
// while XMP keeps them apart.
func TestSaveJoinsAuthorsInTheInformationDictionary(t *testing.T) {
	path := pdftest.Build(t, pdftest.Doc{Info: map[string]string{}})

	mustSave(t, path, func(b *pdf.Book) { b.Authors = []string{"Ann One", "Bo Two"} })

	if !bytes.Contains(read(t, path), []byte("/Author (Ann One; Bo Two)")) {
		t.Error("the dictionary's Author is not the joined names")
	}

	if got := open(t, path).Authors; !reflect.DeepEqual(got, []string{"Ann One", "Bo Two"}) {
		t.Errorf("Authors = %q", got)
	}
}

func TestSaveWritesNonASCIIAsUTF16(t *testing.T) {
	path := pdftest.Build(t, pdftest.Doc{})

	mustSave(t, path, func(b *pdf.Book) { b.Title = "Tïtle (1)" })

	if !bytes.Contains(read(t, path), []byte("/Title <FEFF005400EF0074006C00650020002800310029>")) {
		t.Error("the title is not written as UTF-16BE")
	}

	if got := open(t, path).Title; got != "Tïtle (1)" {
		t.Errorf("Title = %q", got)
	}
}

func TestSaveSetsModificationDatesAndANewSecondID(t *testing.T) {
	eachLayout(t, richDoc, func(t *testing.T, path string) {
		mustSave(t, path, func(b *pdf.Book) { b.Title = "New" })

		out := string(read(t, path))
		for _, want := range []string{"/ModDate (D:", "<xmp:ModifyDate>", "<xmp:MetadataDate>"} {
			if !strings.Contains(out, want) {
				t.Errorf("the update has no %s", want)
			}
		}

		const id = "<00112233445566778899AABBCCDDEEFF>"
		if strings.Count(out, id) != 3 {
			t.Errorf("want the first ID kept and the second replaced, got %d copies", strings.Count(out, id))
		}
	})
}

func TestSaveTwiceChainsUpdates(t *testing.T) {
	eachLayout(t, richDoc, func(t *testing.T, path string) {
		b := open(t, path)

		b.Title = "First"
		if err := b.Save(); err != nil {
			t.Fatal(err)
		}

		b.Title = "Second"
		if err := b.Save(); err != nil {
			t.Fatal(err)
		}

		if got := open(t, path).Title; got != "Second" {
			t.Errorf("Title = %q", got)
		}

		if n := strings.Count(string(read(t, path)), "%%EOF"); n != 3 {
			t.Errorf("%d revisions, want 3", n)
		}
	})
}

func TestSaveKeepsTheFileModeAndLeavesNoTemporaryFile(t *testing.T) {
	path := pdftest.Build(t, richDoc)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	mustSave(t, path, func(b *pdf.Book) { b.Title = "New" })

	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, %v, want 0640", fi.Mode().Perm(), err)
	}

	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), pdf.SaveTempPattern)); len(left) != 0 {
		t.Errorf("left behind %v", left)
	}
}

func TestSaveRefusesASignedOrCertifiedPDF(t *testing.T) {
	for name, catalog := range map[string]string{
		"signed":    "/AcroForm <</Fields [] /SigFlags 3>>",
		"certified": "/Perms <</DocMDP 9 0 R>>",
	} {
		t.Run(name, func(t *testing.T) {
			path := pdftest.Build(t, pdftest.Doc{Info: map[string]string{"Title": "(T)"}, Catalog: catalog})
			before := read(t, path)

			if err := save(t, path, func(b *pdf.Book) { b.Title = "New" }); err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Errorf("Save err = %v, want a refusal", err)
			}

			if !bytes.Equal(before, read(t, path)) {
				t.Error("a refused save changed the file")
			}
		})
	}
}

// Rewriting a packet that cannot be parsed would lose whatever it holds.
func TestSaveRefusesToReplaceAnUnparseablePacket(t *testing.T) {
	path := pdftest.Build(t, pdftest.Doc{XMP: "<x:xmpmeta><unclosed>"})

	if err := save(t, path, func(b *pdf.Book) { b.Title = "New" }); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("Save err = %v, want a refusal", err)
	}
}

func TestSaveRefusesAnUnrepresentableCharacter(t *testing.T) {
	const bad = "bad\x01value"

	typ := reflect.TypeFor[pdf.Metadata]()
	for i := range typ.NumField() {
		t.Run(typ.Field(i).Name, func(t *testing.T) {
			path := pdftest.Build(t, richDoc)
			before := read(t, path)

			err := save(t, path, func(b *pdf.Book) {
				f := reflect.ValueOf(&b.Metadata).Elem().Field(i)
				switch f.Kind() {
				case reflect.Slice:
					f.Set(reflect.ValueOf([]string{"fine", bad}))
				case reflect.Pointer:
					f.Set(reflect.ValueOf(&pdf.Series{Name: "fine", Index: bad}))
				default:
					f.SetString(bad)
				}
			})
			if err == nil || !strings.Contains(err.Error(), "XML 1.0") {
				t.Errorf("Save err = %v, want the XML 1.0 refusal", err)
			}

			if !bytes.Equal(before, read(t, path)) {
				t.Error("a refused save changed the file")
			}
		})
	}

	path := pdftest.Build(t, richDoc)
	mustSave(t, path, func(b *pdf.Book) { b.Description = "one\ntwo\tthree\r" })

	if got := open(t, path).Description; got != "one\ntwo\tthree\r" {
		t.Errorf("Description = %q", got)
	}
}

// Poppler is an independent reader. Where it is installed, it must read what
// Save wrote, in each layout and across a chain of updates.
func TestSavedFileReadsInPoppler(t *testing.T) {
	pdfinfo, err := exec.LookPath("pdfinfo")
	if err != nil {
		t.Skip("pdfinfo not installed")
	}

	eachLayout(t, richDoc, func(t *testing.T, path string) {
		mustSave(t, path, func(b *pdf.Book) { b.Title = "Poppler One" })
		mustSave(t, path, func(b *pdf.Book) { b.Authors = []string{"A", "B"} })

		out, err := exec.Command(pdfinfo, path).CombinedOutput()
		if err != nil {
			t.Fatalf("pdfinfo: %v\n%s", err, out)
		}

		for _, want := range []string{"Title:           Poppler One", "Author:          A; B", "Metadata Stream: yes"} {
			if !strings.Contains(string(out), want) {
				t.Errorf("pdfinfo output lacks %q:\n%s", want, out)
			}
		}

		if strings.Contains(string(out), "Syntax Error") {
			t.Errorf("pdfinfo reports syntax errors:\n%s", out)
		}
	})
}

// §7.3.4.2: a literal string escapes its parentheses and backslashes.
func TestSaveEscapesALiteralString(t *testing.T) {
	path := pdftest.Build(t, pdftest.Doc{})

	mustSave(t, path, func(b *pdf.Book) { b.Title = `a (b) \ c)` })

	if !bytes.Contains(read(t, path), []byte(`/Title (a \(b\) \\ c\))`)) {
		t.Error("the title is not an escaped literal string")
	}

	if got := open(t, path).Title; got != `a (b) \ c)` {
		t.Errorf("Title = %q", got)
	}
}
