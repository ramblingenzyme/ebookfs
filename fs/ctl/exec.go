package ctl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/fs/textfmt"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// execute parses a command line, dispatches it, and returns the result string.
func execute(cmd string, lib library.Library, reg *registry.BookRegistry, cmdLog *CommandLog) string {
	name, args, err := parseCommand(cmd)
	if err != nil {
		r := fmt.Sprintf("error: %v", err)
		cmdLog.Append(cmd, r)
		return r
	}

	r := dispatch(name, args, lib, reg)
	cmdLog.Append(cmd, r)
	return r
}

func dispatch(name string, args []string, lib library.Library, reg *registry.BookRegistry) string {
	switch name {
	case "add-tag":
		return addTag(args, lib, reg)
	case "remove-tag":
		return removeTag(args, lib, reg)
	case "set-status":
		return setStatus(args, lib, reg)
	case "set-rating":
		return setRating(args, lib, reg)
	case "delete":
		return deleteBook(args, lib, reg)
	case "reindex":
		return reindexCmd(args, lib)
	case "rename-tag":
		return renameTag(args, lib, reg)
	case "rename-author":
		return renameAuthor(args, lib, reg)
	case "rename-series":
		return renameSeries(args, lib, reg)
	default:
		return fmt.Sprintf("error: unknown command %q", name)
	}
}

// --- id-spec commands ---

func addTag(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: add-tag <tag> <id-spec>"
	}
	tag, spec := args[0], args[1]

	query, err := parseSelection(spec)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	return editSelection(query, lib, reg, func(b *library.Book) *library.Edits {
		if slices.Contains(b.Tags(), tag) {
			return nil // already has tag
		}
		newTags := append(slices.Clone(b.Tags()), tag)
		return &library.Edits{Tags: &newTags}
	})
}

func removeTag(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: remove-tag <tag> <id-spec>"
	}
	tag, spec := args[0], args[1]

	query, err := parseSelection(spec)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	return editSelection(query, lib, reg, func(b *library.Book) *library.Edits {
		if !slices.Contains(b.Tags(), tag) {
			return nil // doesn't have tag
		}
		newTags := slices.DeleteFunc(slices.Clone(b.Tags()), func(t string) bool {
			return t == tag
		})
		return &library.Edits{Tags: &newTags}
	})
}

func setStatus(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: set-status <status> <id-spec>"
	}
	status, spec := args[0], args[1]

	query, err := parseSelection(spec)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	return editSelection(query, lib, reg, func(b *library.Book) *library.Edits {
		if b.Status() == status {
			return nil
		}
		return &library.Edits{Status: &status}
	})
}

func setRating(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: set-rating <rating> <id-spec>"
	}
	raw, spec := args[0], args[1]

	rating, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Sprintf("error: invalid rating %q", raw)
	}

	query, err := parseSelection(spec)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	return editSelection(query, lib, reg, func(b *library.Book) *library.Edits {
		if b.Rating() == rating {
			return nil
		}
		return &library.Edits{Rating: &rating}
	})
}

// --- single-book commands ---

func deleteBook(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 1 {
		return "usage: delete <id>"
	}
	id64, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Sprintf("error: invalid id %q", args[0])
	}

	if err := lib.Delete(id64); err != nil {
		return fmt.Sprintf("error: book %d: %v", id64, err)
	}
	reg.Remove(id64)
	return fmt.Sprintf("ok: book %d deleted", id64)
}

func reindexCmd(args []string, lib library.Library) string {
	if len(args) != 0 {
		return "usage: reindex"
	}
	if err := lib.Reindex(); err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return "ok: index rebuilt"
}

// --- entity management ---

// renameTag replaces the tag old with new on every book that carries old. If a
// book already has new, old is simply dropped rather than duplicated — so
// renaming a tag onto an existing one merges the two. There is no separate
// merge command: this is the merge.
func renameTag(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: rename-tag <old> <new>"
	}
	old, new := args[0], args[1]

	books, err := lib.Search(library.Query{Tags: []string{old}})
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	var affected int64
	var errs []string

	for _, b := range books {
		var updated []string
		if slices.Contains(b.Tags(), new) {
			// Book already has the new tag; just remove the old one.
			updated = slices.DeleteFunc(slices.Clone(b.Tags()), func(t string) bool {
				return t == old
			})
		} else {
			updated = slices.Clone(b.Tags())
			for i, t := range updated {
				if t == old {
					updated[i] = new
				}
			}
		}
		if err := reg.Edit(b.ID(), library.Edits{Tags: &updated}); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", b.ID(), err))
		} else {
			affected++
		}
	}

	return formatResult("renamed", affected, 0, errs)
}

func renameAuthor(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: rename-author <old> <new>"
	}
	old, rawNew := args[0], args[1]

	// Parse new author (supports "Name | Sort" format).
	newAuthor := textfmt.ParseAuthor(rawNew)
	if newAuthor.Name == "" {
		return "error: new author name must not be empty"
	}

	books, err := lib.Search(library.Query{Authors: []string{old}})
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	var affected int64
	var errs []string

	for _, b := range books {
		matched := false
		updated := slices.Clone(b.Authors())
		for i, a := range updated {
			if a.Name == old || a.SortName == old {
				updated[i] = newAuthor
				matched = true
			}
		}
		if !matched {
			continue
		}
		// Renaming onto an author the book already has (or renaming two of its
		// authors to the same person) would duplicate that author; dedupe so the
		// rename doubles as a merge, like rename-tag. This also collapses any
		// duplicate authors the book already carried — broader than the rename
		// strictly implies, but harmless: only books the rename matched are
		// rewritten at all.
		updated = dedupeAuthors(updated)
		if err := reg.Edit(b.ID(), library.Edits{Authors: &updated}); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", b.ID(), err))
		} else {
			affected++
		}
	}

	return formatResult("renamed", affected, 0, errs)
}

func renameSeries(args []string, lib library.Library, reg *registry.BookRegistry) string {
	if len(args) != 2 {
		return "usage: rename-series <old> <new>"
	}
	old, new := args[0], args[1]

	books, err := lib.Search(library.Query{Series: []string{old}})
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	var affected int64
	var errs []string

	for _, b := range books {
		if err := reg.Edit(b.ID(), library.Edits{Series: &new}); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", b.ID(), err))
		} else {
			affected++
		}
	}

	return formatResult("renamed", affected, 0, errs)
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
func dedupeAuthors(authors []model.Author) []model.Author {
	seen := make(map[string]bool, len(authors))
	out := make([]model.Author, 0, len(authors))
	for _, a := range authors {
		if seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	return out
}

// editSelection applies editFn to each book the selection addresses. It runs one
// library.Search(query) rather than hydrating the whole library to filter it
// down. When the query is a bare id list, an id naming no book is reported (so
// a typo isn't counted as success) and a duplicated id is collapsed to a single
// visit; otherwise every returned book is visited.
func editSelection(query library.Query, lib library.Library, reg *registry.BookRegistry, editFn func(*library.Book) *library.Edits) string {
	books, err := lib.Search(query)
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
		if err := reg.Edit(id, *edits); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", id, err))
		} else {
			affected++
		}
	}

	return formatResult("edited", affected, skipped, errs)
}

// formatResult builds a human-readable result string.
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
