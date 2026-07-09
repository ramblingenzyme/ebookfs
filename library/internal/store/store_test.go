package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// TestMoveSameDirectoryRenamesFilename reproduces a production bug: a book
// ingested before FAT sanitization was consistently applied can have an
// on-disk epub filename containing FAT-illegal characters (e.g. ':'), while
// its LibraryPath (built from the raw, unsanitized title) still matches what
// Layout() recomputes. Move must treat this as an in-place filename rename,
// not a directory move — the "destination" directory is the book's own
// current one, so the old "destination already exists" check falsely tripped
// on every such edit.
func TestMoveSameDirectoryRenamesFilename(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyName := "Some Title: With Colon - Some Author.epub"
	if err := os.WriteFile(filepath.Join(dir, legacyName), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	from := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: legacyName}
	sanitizedName := "Some Title- With Colon - Some Author.epub"
	to := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: sanitizedName}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, sanitizedName)); err != nil {
		t.Errorf("renamed epub not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyName)); !os.IsNotExist(err) {
		t.Errorf("legacy filename still present after rename")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("book directory should be unchanged: %v", err)
	}
}

// TestMoveCleansUpEmptyOldAuthorDir reproduces a live production issue: after
// moving a book to a new author directory (e.g. a title/author edit), the old
// author directory was left behind once it held no other books. Move must
// clean it up the same way Delete already does for its parent.
func TestMoveCleansUpEmptyOldAuthorDir(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	oldDir := filepath.Join(root, "Wells, M", "Some Title (1)")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	epubName := "Some Title - Wells.epub"
	if err := os.WriteFile(filepath.Join(oldDir, epubName), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	from := model.Location{LibraryPath: "Wells, M/Some Title (1)", EpubFilename: epubName}
	to := model.Location{LibraryPath: "Wells, Martha/Some Title (1)", EpubFilename: epubName}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "Wells, Martha", "Some Title (1)", epubName)); err != nil {
		t.Errorf("book not found at new location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Wells, M")); !os.IsNotExist(err) {
		t.Errorf("old author directory should have been removed, got err=%v", err)
	}
}

// TestMoveKeepsOldAuthorDirWithRemainingBooks ensures Move's cleanup only
// removes the old author directory when it's actually empty — sibling books
// by the same author must survive.
func TestMoveKeepsOldAuthorDirWithRemainingBooks(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	oldAuthorDir := filepath.Join(root, "Wells, M")
	movingDir := filepath.Join(oldAuthorDir, "Some Title (1)")
	if err := os.MkdirAll(movingDir, 0755); err != nil {
		t.Fatal(err)
	}
	epubName := "Some Title - Wells.epub"
	if err := os.WriteFile(filepath.Join(movingDir, epubName), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	siblingDir := filepath.Join(oldAuthorDir, "Other Title (2)")
	if err := os.MkdirAll(siblingDir, 0755); err != nil {
		t.Fatal(err)
	}

	from := model.Location{LibraryPath: "Wells, M/Some Title (1)", EpubFilename: epubName}
	to := model.Location{LibraryPath: "Wells, Martha/Some Title (1)", EpubFilename: epubName}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(siblingDir); err != nil {
		t.Errorf("sibling book directory should survive: %v", err)
	}
}

// TestMoveSameLocationNoop covers the defensive no-op branch: Move must not
// error or touch the filesystem when from and to are identical, even though
// Edit's guard currently never calls Move in that case.
func TestMoveSameLocationNoop(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Some Title - Some Author.epub"), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	loc := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: "Some Title - Some Author.epub"}
	if err := s.Move(loc, loc); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Some Title - Some Author.epub")); err != nil {
		t.Errorf("epub should be unchanged: %v", err)
	}
}
