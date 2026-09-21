package util

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// BuildTestEpub writes the mimetype entry first and uncompressed, which OCF
// 3.3 §4.3.3 requires of a valid epub.
func BuildTestEpub(t testing.TB, title string, authors ...string) []byte {
	t.Helper()
	if len(authors) == 0 {
		authors = []string{"Alice"}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mt, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	mt.Write([]byte("application/epub+zip"))

	var creatorEls strings.Builder
	for i, a := range authors {
		creatorEls.WriteString(fmt.Sprintf("    <dc:creator id=\"c%d\">%s</dc:creator>\n", i+1, a))
	}

	//goland:noinspection HttpUrlsUsage
	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"content.opf": `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">ebookfs-test-1</dc:identifier>
    <dc:title>` + title + `</dc:title>
` + creatorEls.String() + `    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="cover" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
</package>`,
		"cover.jpg": "placeholder-cover-bytes",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// LibraryConfig mirrors library.Config, which this package cannot import (see
// the package doc), so a caller writes library.Config(util.TestConfig(t)). A
// field added there and not here breaks that conversion at compile time.
type LibraryConfig struct {
	Root      string
	InboxTemp string
	IndexPath string
}

// TestConfig exists for a test that reopens a library across a restart.
func TestConfig(t testing.TB) LibraryConfig {
	t.Helper()
	dir := t.TempDir()
	return LibraryConfig{
		Root:      filepath.Join(dir, "root"),
		InboxTemp: filepath.Join(dir, "inbox-tmp"),
		IndexPath: filepath.Join(dir, "index.db"),
	}
}
