package fs

import (
	"errors"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
)

func TestNewInboxDir(t *testing.T) {
	f := newTestFS(t)
	d := newInboxDir(f)

	s := d.Stat()
	if s.Name != "inbox" {
		t.Errorf("inboxDir name = %q, want %q", s.Name, "inbox")
	}
	if s.Mode&proto.DMDIR == 0 {
		t.Error("inboxDir should have DMDIR flag set")
	}
}

func TestInboxCreateFile_Success(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{}
	cf := inboxCreateFile(lib, nil)

	inbox := newInboxDir(f)
	file, err := cf(f, inbox, "glenda", "test.epub", 0644, 0)
	if err != nil {
		t.Fatalf("inboxCreateFile: %v", err)
	}
	if file == nil {
		t.Fatal("inboxCreateFile returned nil file")
	}

	// File should be added as a child of the inbox dir.
	if _, ok := inbox.Children()["test.epub"]; !ok {
		t.Error("inbox dir should contain 'test.epub'")
	}
}

func TestInboxCreateFile_WrongParent(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{}
	cf := inboxCreateFile(lib, nil)

	// Pass a plain StaticDir as parent instead of *inboxDir.
	parent := fs.NewStaticDir(newStat(f, "wrong", 0755|proto.DMDIR))
	_, err := cf(f, parent, "glenda", "test.epub", 0644, 0)
	if err == nil {
		t.Fatal("expected error for non-inboxDir parent")
	}
	if err.Error() != "not under inbox" {
		t.Errorf("got error %q, want %q", err.Error(), "not under inbox")
	}
}

func TestInboxFileOpenCreateIngestError(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		createIngestFn: func() (library.IngestHandle, error) {
			return nil, errors.New("CreateIngest failed")
		},
	}
	inf := newInboxFile(f, lib, "test.epub", 0644, nil)

	err := inf.Open(1, proto.Mode(0))
	if err == nil {
		t.Fatal("expected error from CreateIngest")
	}
}
