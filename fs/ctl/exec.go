package ctl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// execute parses a command line, dispatches it, and returns the result string.
func execute(cmd string, lib library.Library, reg *registry.BookRegistry, cmdLog *CommandLog) string {
	p, err := parseCommand(cmd)
	if err != nil {
		r := fmt.Sprintf("error: %v", err)
		cmdLog.Append(cmd, r)
		return r
	}

	r := dispatch(p, lib, reg)
	cmdLog.Append(cmd, r)
	return r
}

func dispatch(p parsedCmd, lib library.Library, reg *registry.BookRegistry) string {
	switch p.name {
	case "add-tag":
		return addTag(p.args, lib, reg)
	case "remove-tag":
		return removeTag(p.args, lib, reg)
	case "set-status":
		return setStatus(p.args, lib, reg)
	case "set-rating":
		return setRating(p.args, lib, reg)
	case "delete":
		return deleteBook(p.args, lib, reg)
	case "reindex":
		return reindexCmd(p.args, lib)
	case "rename-tag":
		return renameTag(p.args, lib, reg)
	case "rename-author":
		return renameAuthor(p.args, lib, reg)
	case "rename-series":
		return renameSeries(p.args, lib, reg)
	default:
		return fmt.Sprintf("error: unknown command %q", p.name)
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

	return editSelection(query, lib, reg, func(b *model.Book) *model.Edits {
		if slices.Contains(b.Meta.Tags, tag) {
			return nil // already has tag
		}
		newTags := append(slices.Clone(b.Meta.Tags), tag)
		return &model.Edits{Tags: &newTags}
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

	return editSelection(query, lib, reg, func(b *model.Book) *model.Edits {
		if !slices.Contains(b.Meta.Tags, tag) {
			return nil // doesn't have tag
		}
		newTags := slices.DeleteFunc(slices.Clone(b.Meta.Tags), func(t string) bool {
			return t == tag
		})
		return &model.Edits{Tags: &newTags}
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

	return editSelection(query, lib, reg, func(b *model.Book) *model.Edits {
		if b.Meta.Status == status {
			return nil
		}
		return &model.Edits{Status: &status}
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

	return editSelection(query, lib, reg, func(b *model.Book) *model.Edits {
		if b.Meta.Rating == rating {
			return nil
		}
		return &model.Edits{Rating: &rating}
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

	books, err := lib.Query(model.Filter{Tag: old})
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	var affected int64
	var errs []string

	for _, b := range books {
		if !slices.Contains(b.Meta.Tags, old) {
			continue
		}
		var updated []string
		if slices.Contains(b.Meta.Tags, new) {
			// Book already has the new tag; just remove the old one.
			updated = slices.DeleteFunc(slices.Clone(b.Meta.Tags), func(t string) bool {
				return t == old
			})
		} else {
			updated = slices.Clone(b.Meta.Tags)
			for i, t := range updated {
				if t == old {
					updated[i] = new
				}
			}
		}
		if err := reg.CommitEdit(b.Meta.ID, model.Edits{Tags: &updated}); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", b.Meta.ID, err))
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
	newAuthor := model.ParseAuthor(rawNew)
	if newAuthor.Name == "" {
		return "error: new author name must not be empty"
	}

	books, err := lib.Query(model.Filter{})
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	var affected int64
	var errs []string

	for _, b := range books {
		matched := false
		updated := slices.Clone(b.Authors)
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
		if err := reg.CommitEdit(b.Meta.ID, model.Edits{Authors: &updated}); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", b.Meta.ID, err))
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

	books, err := lib.Query(model.Filter{Series: old})
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	var affected int64
	var errs []string

	for _, b := range books {
		if b.Series == nil || b.Series.Name != old {
			continue
		}
		if err := reg.CommitEdit(b.Meta.ID, model.Edits{Series: &new}); err != nil {
			errs = append(errs, fmt.Sprintf("book %d: %v", b.Meta.ID, err))
		} else {
			affected++
		}
	}

	return formatResult("renamed", affected, 0, errs)
}

// --- helpers ---

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
// down. When the query names explicit ids, an id naming no book is reported (so
// a typo isn't counted as success) and a duplicated id is collapsed to a single
// visit; when the query selects all books ("*"), every returned book is visited.
func editSelection(query model.Query, lib library.Library, reg *registry.BookRegistry, editFn func(*model.Book) *model.Edits) string {
	books, err := lib.Search(query)
	if err != nil {
		return fmt.Sprintf("error: query failed: %v", err)
	}

	byID := make(map[int64]*model.Book, len(books))
	for _, b := range books {
		byID[b.Meta.ID] = b
	}

	// Walk the explicit id list when there is one, so a typo surfaces as "not
	// found"; otherwise walk the ids the query actually returned.
	visit := query.IDs
	if len(visit) == 0 {
		visit = make([]int64, 0, len(books))
		for _, b := range books {
			visit = append(visit, b.Meta.ID)
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
		if err := reg.CommitEdit(id, *edits); err != nil {
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
