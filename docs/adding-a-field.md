# Adding a field

A book field crosses every layer, from the OPF to a 9P file, and several of
the places it must be added fail silently when missed. This is the list, in
the order the data flows. Subjects (multi-valued) and publisher
(single-valued) are the worked examples to copy.

## A book metadata field

### The epub format: `pkg/epub`

`pkg/epub` is a standalone library (see AGENTS.md), so the field goes here only
as the file states it, with no ebookfs rule attached.

1. **Read and write the OPF.** A single-valued Dublin Core element needs only a
   getter and a `Set` method on `opf.Package` (`doc.go`). A field with an
   encoding of its own gets a `field_<name>.go` with `get` and `set`, keeping
   its EPUB 2 and EPUB 3 branches visible. Add it to `opf.Metadata`.
2. **Expose it on `epub.Metadata`** (`book.go`), and copy it in `newBook`.
3. **Add it to `Book.take`, cloning a slice.** A field missing there compares
   against its zero value, so clearing it is never saved. A slice copied
   without cloning shares the caller's array, so `Save` misses an in-place
   change.
4. **Add it to `moved` and `write`** (`save.go`). `moved.changed` needs no edit.

`TestSaveRoundTripsEveryMetadataField` fills every `Metadata` field by
reflection, so it fails when any of steps 2–4 is missed.

### The library: `pkg/library` and `internal/book`

5. **`book.Bib`** (`internal/book/book.go`) holds the parsed value.
6. **`book.Edits`** (`internal/book/edits.go`): add the pointer field.
   `HasBibEdits` counts a new field as an OPF edit by itself. A field stored in
   `meta.toml` instead must be cleared there, or every edit of it opens the
   epub, and a signed epub refuses it.
7. **`Validate`**: reject what the index or a path cannot hold. A multi-valued
   field rejects repeats, since the index keys each entry on (book, value).
8. **The translation layer** (`pkg/library/internal/epub`): read it in `bib`
   (`parse.go`), and apply it in `apply` (`write.go`). A multi-valued field is
   passed through `book.FirstOf` on read, so a file that repeats a value cannot
   break the index.
9. **`ImmutableBook`** (`immutable.go`): add a getter, cloning a slice.

### The index: `pkg/library/internal/index`

10. **Schema** (`schema.sql`). A single-valued field is a column on `books`. A
    multi-valued one gets its own table, a `book_<name>` join table with
    `ON DELETE CASCADE` and a `position` column if order matters, and an
    orphan cleanup in `cleanupOrphans`.
11. **Bump `schemaVersion`** (`index.go`). The next start drops the index and
    rebuilds it from disk (DECISIONS.md #2), so no migration is written.
12. **Queries** (`queries.sql`), then regenerate with
    `cd pkg/library/internal/index && sqlc generate`.
13. **Writes**: `bookParams` for a column, or a `replace<Name>` in
    `putRelations` for a table. Add a new table to the `DELETE` list in
    `rebuildTx`.
14. **Reads**: the column goes in `bookQuery.sql` and `scan.go`. A table is
    loaded in `hydrateBooks`, ordered by `position` when it has one.

### The frontends

15. **9P**: add the field file to `fields` (`internal/fs/book/fields.go`), and
    to the tree in `docs/namespace.md`. A grouping view, like `by-subjects/`,
    is a `keyedDir` registered in `fs.New`.
16. **OPDS**: map it onto the publication in `toPublication`, if OPDS has a
    slot for it.

If the field should be searchable, continue with the next section.

## A query filter

A filter is answered twice. `index.Search` answers it in SQL, for ctl and
OPDS. The `search/` view answers it in Go, over the registry's books. The two
must agree, or a bulk edit and the listing a user checked it against select
different books.

1. **`index.Query`** (`query.go`): add the field.
2. **SQL**: add an `addIn` clause in `index.Search` (`search.go`).
3. **Go**: add a `matches<Name>` in `internal/fs/views/match.go`, and call it
   from `makeMatchesFn`.
4. **Syntax**: add the prefix to `textfmt.ParseQuery`. It serves both
   `search/` and ctl's id-specs.
5. **Parity test**: add cases to `TestSearchMatcherAgreesWithSQL`, and set the
   field in its every-field query. That test fails until the field is set
   there, and the case then runs the field through both matchers.

ctl's check for a bare id list (`idsOnly`) counts every `Query` field by
itself, so it needs no change.
