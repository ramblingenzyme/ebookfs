// Reading a result back: the entries of a written epub, and the elements of its
// package document. Every helper here fails the test on a lookup that finds
// nothing, and names what was there instead.

package epubtest

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func Metadata(t *testing.T, path string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(ReadEntry(t, path, OPFPath)); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	md := doc.FindElement("//metadata")
	if md == nil {
		t.Fatal("result has no <metadata>")
	}
	return md
}

// ReadEntryFromFile reports absence; ReadEntry fails the test on it.
func ReadEntryFromFile(t *testing.T, path, name string) ([]byte, bool) {
	t.Helper()
	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()
	for _, f := range zrc.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b := new(bytes.Buffer)
			if _, err := b.ReadFrom(rc); err != nil {
				t.Fatal(err)
			}
			return b.Bytes(), true
		}
	}
	return nil, false
}

func ReadEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	b, ok := ReadEntryFromFile(t, path, name)
	if !ok {
		t.Fatalf("entry %q not found", name)
	}
	return b
}

// LegacyMeta and Property read the two encodings of the same idea. An EPUB 2
// <meta name="x" content="v"/> carries its value in an attribute; an EPUB 3
// <meta property="x">v</meta> carries it as text. Which one a file uses is what
// half these tests turn on, so the difference is named here instead of respelled
// at every assertion.
//
// Both return a value rather than comparing it. The caller's own message names
// the spec consequence, and "%v" on an *etree.Element prints etree's struct
// rather than the value in question.
func LegacyMeta(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return AttrOf(t, root, path, "content")
}

func Property(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return elemAt(t, root, path).Text()
}

// AttrOf and TextOf are the same lookup for the elements other than <meta>.
func AttrOf(t *testing.T, root *etree.Element, path, attr string) string {
	t.Helper()
	return elemAt(t, root, path).SelectAttrValue(attr, "")
}

func TextOf(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return elemAt(t, root, path).Text()
}

func elemAt(t *testing.T, root *etree.Element, path string) *etree.Element {
	t.Helper()
	el := root.FindElement(path)
	if el == nil {
		t.Fatalf("nothing matches %s; <metadata> holds %s", path, childTags(root))
	}
	return el
}

// childTags names what is there, since a failed lookup is most often an element
// written under the wrong parent rather than one that vanished.
func childTags(root *etree.Element) string {
	var tags []string
	for _, c := range root.ChildElements() {
		tags = append(tags, c.Tag)
	}
	return strings.Join(tags, ", ")
}
