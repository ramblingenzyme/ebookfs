// Reading a result back: the entries of a written epub, and the elements of its
// package document. Every helper here fails the test on a lookup that finds
// nothing, and names what was there instead, since a failed lookup is most
// often an element written under the wrong parent rather than one that vanished.

package epub_test

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func metadata(t *testing.T, path string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, path, opfPath)); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	md := doc.FindElement("//metadata")
	if md == nil {
		t.Fatal("result has no <metadata>")
	}
	return md
}

// readEntryFromFile returns the entry's bytes and whether it was present.
// readEntry is the same lookup for the common case where absence should fail
// the test.
func readEntryFromFile(t *testing.T, path, name string) ([]byte, bool) {
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

// readEntry returns the entry's bytes, failing the test when it is absent.
// readEntryFromFile is the same lookup for tests that need to assert on absence.
func readEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	b, ok := readEntryFromFile(t, path, name)
	if !ok {
		t.Fatalf("entry %q not found", name)
	}
	return b
}

// legacyMeta and property read the two encodings of the same idea. An EPUB 2
// <meta name="x" content="v"/> carries its value in an attribute; an EPUB 3
// <meta property="x">v</meta> carries it as text. Which one a file uses is what
// half these tests turn on, so the difference is named here instead of respelled
// at every assertion.
//
// Both fail when nothing matches, and both return a value rather than comparing
// it: the caller's own message names the spec consequence, and "%v" on an
// *etree.Element prints etree's struct rather than the value in question.
func legacyMeta(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return attrOf(t, root, path, "content")
}

func property(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return elemAt(t, root, path).Text()
}

// attrOf and elemAt are the same lookup for the elements that are not <meta>:
// a dc element's text, or an attribute the spec puts somewhere else.
func attrOf(t *testing.T, root *etree.Element, path, attr string) string {
	t.Helper()
	return elemAt(t, root, path).SelectAttrValue(attr, "")
}

func textOf(t *testing.T, root *etree.Element, path string) string {
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

// childTags names what is there, since a failed lookup is most often a element
// written under the wrong parent rather than one that vanished.
func childTags(root *etree.Element) string {
	var tags []string
	for _, c := range root.ChildElements() {
		tags = append(tags, c.Tag)
	}
	return strings.Join(tags, ", ")
}
