package pdf_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/pdftest"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf"
)

var layouts = []struct {
	name   string
	layout pdftest.Layout
}{
	{"table", pdftest.Table},
	{"stream", pdftest.Stream},
}

// eachLayout runs f once per layout, with doc built in it.
func eachLayout(t *testing.T, doc pdftest.Doc, f func(t *testing.T, path string)) {
	t.Helper()

	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			doc.Layout = l.layout
			f(t, pdftest.Build(t, doc))
		})
	}
}

func open(t *testing.T, path string) *pdf.Book {
	t.Helper()

	b, err := pdf.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { b.Close() })

	return b
}

func save(t *testing.T, path string, edit func(*pdf.Book)) error {
	t.Helper()

	b := open(t, path)
	edit(b)

	return b.Save()
}

func mustSave(t *testing.T, path string, edit func(*pdf.Book)) {
	t.Helper()

	if err := save(t, path, edit); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func fillMetadata(t *testing.T, m *pdf.Metadata) {
	t.Helper()

	v := reflect.ValueOf(m).Elem()
	for i := range v.NumField() {
		f, name := v.Field(i), v.Type().Field(i).Name
		switch f.Interface().(type) {
		case string:
			f.SetString(name + " välue")
		case []string:
			f.Set(reflect.ValueOf([]string{name + " one", name + " two"}))
		default:
			t.Fatalf("Metadata.%s is a %s, which fillMetadata has no case for", name, f.Type())
		}
	}
}

// mutateSlices changes the first element of every slice field in place.
func mutateSlices(m *pdf.Metadata) {
	for _, f := range reflect.ValueOf(m).Elem().Fields() {
		if f.Kind() == reflect.Slice && f.Len() > 0 {
			f.Index(0).SetString(f.Index(0).String() + " changed")
		}
	}
}
