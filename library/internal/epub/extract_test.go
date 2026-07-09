package epub

import (
	"strings"
	"testing"
)

func TestExtractOPF(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))

	got, err := ExtractOPF(path)
	if err != nil {
		t.Fatalf("ExtractOPF: %v", err)
	}

	if !strings.Contains(string(got), "Original Title") {
		t.Errorf("OPF should contain title, got: %s", got)
	}
	if !strings.Contains(string(got), "Jane Doe") {
		t.Errorf("OPF should contain author, got: %s", got)
	}
}

func TestExtractOPFInvalidZip(t *testing.T) {
	path := writeEpub(t, nil) // no entries, not a valid epub
	_, err := ExtractOPF(path)
	if err == nil {
		t.Fatal("expected error for epub with no OPF")
	}
}

func TestExtractOPFNoContainer(t *testing.T) {
	path := writeEpub(t, []entry{
		{name: "mimetype", data: []byte("application/epub+zip"), store: true},
	})
	_, err := ExtractOPF(path)
	if err == nil {
		t.Fatal("expected error for epub with no container.xml")
	}
}
