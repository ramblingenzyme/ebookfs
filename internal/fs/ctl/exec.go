package ctl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/fs/textfmt"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// SearchDeleter is the half of the library this package uses. Edits are absent
// because they go through the registry instead, so the 9P tree is re-rendered
// with them.
type SearchDeleter interface {
	Search(q library.Query) ([]*library.Book, error)
	Delete(id int64) error
}

func (f *CtlFile) execute(cmd string) string {
	name, args, err := parseCommand(cmd)
	if err != nil {
		r := fmt.Sprintf("error: %v", err)
		f.cmdLog.Append(cmd, r)
		return r
	}

	r := f.dispatch(name, args)
	f.cmdLog.Append(cmd, r)
	return r
}

// command is one ctl verb. params spells its arguments the way the usage line
// and the help file both show them, and doubles as the arity: every parameter
// is required, so their count is how many args the handler needs.
//
// A handler takes the file rather than hanging off it: a command acts through
// ctl but is not behaviour of the ctl file, which answers reads and writes.
// Keeping them out of the method set leaves that surface the four 9P methods
// and the machinery below.
//
// The verb's name is the slice entry's Name and nothing else, so dispatch, the
// usage line and the help file cannot disagree about it.
type command struct {
	name   string
	params string
	desc   string
	run    func(*CtlFile, []string) string
}

// The descriptions long enough that escaping them into the table would make
// both unreadable. They render as written.
// statusDesc names the vocabulary through the library rather than spelling it,
// since adding a status must not leave the help file describing the old set.
var statusDesc = "Set reading status for matching books.\nStatus: " + library.StatusList() + "."

const (
	renameTagDesc = `Rename a tag across every book. Renaming <old> onto a
tag that already exists merges the two: books that had
only <old> now have <new>; books that had both drop the
duplicate <old>.`

	renameAuthorDesc = `Rename an author across every book.
<old> is matched against display name OR sort name.
<new> uses the "Name | Sort" format (same as the
authors field file).`
)

// commands is ordered, since the help file lists them in this order. Lookup is
// a scan of eight entries against one admin typing.
var commands = []command{
	{"add-tag", "<tag> <id-spec>", "Add a tag to matching books.", addTag},
	{"remove-tag", "<tag> <id-spec>", "Remove a tag from matching books.", removeTag},
	{"set-status", "<status> <id-spec>", statusDesc, setStatus},
	{"set-rating", "<rating> <id-spec>", "Set rating (0-5) for matching books.", setRating},
	{"delete", "<id>", "Delete a single book by id.", deleteBook},
	{"rename-tag", "<old> <new>", renameTagDesc, renameTag},
	{"rename-author", "<old> <new>", renameAuthorDesc, renameAuthor},
	{"rename-series", "<old> <new>", "Rename a series across every book.", renameSeries},
}

// usage is the line a command answers with when its arguments do not fit.
func (c command) usage() string { return "usage: " + c.name + " " + c.params }

func (c command) arity() int { return len(strings.Fields(c.params)) }

func (f *CtlFile) dispatch(name string, args []string) string {
	for _, c := range commands {
		if c.name != name {
			continue
		}
		if len(args) != c.arity() {
			return c.usage()
		}
		return c.run(f, args)
	}
	return fmt.Sprintf("error: unknown command %q", name)
}

// --- id-spec commands ---

func addTag(f *CtlFile, args []string) string {
	tag := args[0]

	return f.editSpec("edited", args[1], func(b *library.Book) *library.Edits {
		if slices.Contains(b.Tags(), tag) {
			return nil // already has tag
		}
		newTags := append(slices.Clone(b.Tags()), tag)
		return &library.Edits{Tags: &newTags}
	})
}

func removeTag(f *CtlFile, args []string) string {
	tag := args[0]

	return f.editSpec("edited", args[1], func(b *library.Book) *library.Edits {
		if !slices.Contains(b.Tags(), tag) {
			return nil // doesn't have tag
		}
		newTags := slices.DeleteFunc(slices.Clone(b.Tags()), func(t string) bool {
			return t == tag
		})
		return &library.Edits{Tags: &newTags}
	})
}

func setStatus(f *CtlFile, args []string) string {
	status := args[0]

	return f.editSpec("edited", args[1], func(b *library.Book) *library.Edits {
		if b.Status() == status {
			return nil
		}
		return &library.Edits{Status: &status}
	})
}

func setRating(f *CtlFile, args []string) string {
	rating, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return fmt.Sprintf("error: invalid rating %q", args[0])
	}

	return f.editSpec("edited", args[1], func(b *library.Book) *library.Edits {
		if b.Rating() == rating {
			return nil
		}
		return &library.Edits{Rating: &rating}
	})
}

// --- single-book commands ---

func deleteBook(f *CtlFile, args []string) string {
	id64, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Sprintf("error: invalid id %q", args[0])
	}

	if err := f.lib.Delete(id64); err != nil {
		return fmt.Sprintf("error: book %d: %v", id64, err)
	}
	f.reg.Remove(id64)
	return fmt.Sprintf("ok: book %d deleted", id64)
}

// --- entity management ---

// renameTag replaces the tag old with new on every book that carries old. If a
// book already has new, old is dropped rather than duplicated, so
// renaming a tag onto an existing one merges the two. There is no separate
// merge command: this is the merge.
func renameTag(f *CtlFile, args []string) string {
	old, curr := args[0], args[1]

	return f.editSelection("renamed", library.Query{Tags: []string{old}}, func(b *library.Book) *library.Edits {
		updated := slices.Clone(b.Tags())
		if slices.Contains(updated, curr) {
			updated = slices.DeleteFunc(updated, func(t string) bool { return t == old })
		} else {
			for i, t := range updated {
				if t == old {
					updated[i] = curr
				}
			}
		}
		return &library.Edits{Tags: &updated}
	})
}

func renameAuthor(f *CtlFile, args []string) string {
	old := args[0]

	newAuthor := textfmt.ParseAuthor(args[1])
	if newAuthor.Name == "" {
		return "error: new author name must not be empty"
	}

	return f.editSelection("renamed", library.Query{Authors: []string{old}}, func(b *library.Book) *library.Edits {
		matched := false
		updated := slices.Clone(b.Authors())
		for i, a := range updated {
			if a.Name == old || a.SortName == old {
				updated[i] = newAuthor
				matched = true
			}
		}
		// The query matched on name or sort name and so does this, so a book
		// reaching here without a match means the index disagrees with the
		// snapshot. Counted as skipped rather than silently passed over.
		if !matched {
			return nil
		}
		// Renaming onto an author the book already has (or renaming two of its
		// authors to the same person) would duplicate that author; dedupe so the
		// rename doubles as a merge, like rename-tag. This also collapses any
		// duplicate authors the book already carried, broader than the rename
		// strictly implies, but harmless: only books the rename matched are
		// rewritten at all.
		updated = dedupeAuthors(updated)
		return &library.Edits{Authors: &updated}
	})
}

func renameSeries(f *CtlFile, args []string) string {
	old, curr := args[0], args[1]

	return f.editSelection("renamed", library.Query{Series: []string{old}}, func(*library.Book) *library.Edits {
		return &library.Edits{Series: &curr}
	})
}

// --- helpers ---

// idsOnly reports whether q selects by id and nothing else, i.e. it came from a
// bare id-spec ("1,2,3") rather than a query that happens to name ids.
func idsOnly(q library.Query) bool {
	return len(q.IDs) > 0 && len(q.Authors) == 0 && len(q.Tags) == 0 &&
		len(q.Series) == 0 && len(q.Status) == 0 && len(q.Titles) == 0
}

// dedupeAuthors returns authors with duplicate display names removed, keeping
// the first occurrence of each name. rename-author uses it to fold a renamed
// author into a matching one the book already carries instead of duplicating it.
func dedupeAuthors(authors []library.Author) []library.Author {
	seen := make(map[string]bool, len(authors))
	out := make([]library.Author, 0, len(authors))
	for _, a := range authors {
		if seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	return out
}

// editSpec is editSelection for a command that names its books with an id-spec
// rather than building the query itself.
func (f *CtlFile) editSpec(op, spec string, editFn func(*library.Book) *library.Edits) string {
	query, err := parseSelection(spec)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return f.editSelection(op, query, editFn)
}

// editSelection applies editFn to each book the selection addresses, reporting
// the result as op. An editFn returning nil leaves the book alone and counts it
// as skipped, which is how a command says the book is already in the state it
// asked for.
//
// It runs one library.Search(query) rather than hydrating the whole library to
// filter it down. When the query is a bare id list, an id naming no book is
// reported (so a typo isn't counted as success) and a duplicated id is
// collapsed to a single visit; otherwise every returned book is visited.
func (f *CtlFile) editSelection(op string, query library.Query, editFn func(*library.Book) *library.Edits) string {
	books, err := f.lib.Search(query)
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	byID := make(map[int64]*library.Book, len(books))
	for _, b := range books {
		byID[b.ID()] = b
	}

	// Walk the explicit id list when the query is nothing but ids, so a typo
	// surfaces as "not found". Once the query also filters (id:42+status:read),
	// an id absent from the results means "filtered out", not "no such book",
	// so walk what the query returned instead.
	visit := query.IDs
	if !idsOnly(query) {
		visit = make([]int64, 0, len(books))
		for _, b := range books {
			visit = append(visit, b.ID())
		}
	}

	var affected, skipped int64
	var errs []string
	seen := make(map[int64]bool, len(visit))

	for _, id := range visit {
		if seen[id] {
			continue
		}
		seen[id] = true

		b, ok := byID[id]
		if !ok {
			errs = append(errs, fmt.Sprintf("book %d: not found", id))
			continue
		}
		edits := editFn(b)
		if edits == nil {
			skipped++ // already in the requested state
			continue
		}
		if err := f.reg.Edit(id, *edits); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", id, err))
		} else {
			affected++
		}
	}

	return formatResult(op, affected, skipped, errs)
}

func formatResult(op string, affected, skipped int64, errs []string) string {
	var parts []string
	if affected > 0 {
		parts = append(parts, fmt.Sprintf("ok: %d books %s", affected, op))
	} else {
		parts = append(parts, fmt.Sprintf("ok: no books %s", op))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	if len(errs) > 0 {
		parts = append(parts, fmt.Sprintf("errors: %d book(s)", len(errs)))
		for _, e := range errs {
			parts = append(parts, "  "+e)
		}
	}
	return strings.Join(parts, "\n")
}
