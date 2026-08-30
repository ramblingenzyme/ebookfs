# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Book types moved to internal/book; `library.Book` is now an immutable wrapper.** `Book`, `Bib`, `Location`, and `Meta` moved from `library/model` to `internal/book` and are no longer part of the public API. `model.Author` and `model.SeriesRef` remain as type aliases. The old `model.Book` struct (direct field access) is replaced by `library.Book`, an alias for `book.ImmutableBook` — a read-only wrapper that exposes fields via getter methods (`Title()`, `Authors()`, `Status()`, etc.) instead of struct fields. `Library.Search`, `Library.Edit`, and all `Exporter` methods now return or accept `*library.Book` instead of `*model.Book`. Callers must use the getter methods and re-fetch after mutations.
- **Edits moved to library/internal/epub/edits.** The `Edits` type, `ValidationError`, `FieldError`, and validation logic moved from `library/model` to `library/internal/epub/edits` to prevent import cycles and better organize epub-related types. `Validate` is now a package function instead of a method on `Edits`, and takes `*book.Book` instead of `*book.ImmutableBook`. Type aliases in the `library` package (`library.Edits`, `library.ValidationError`, `library.FieldError`) have been added.
- **Status vocabulary consolidated in internal/book.** Reading-status constants (`StatusUnread`, `StatusReading`, `StatusRead`, `StatusAbandoned`), `Statuses` slice, `IsValidStatus()`, and `StatusList()` now live in `internal/book` instead of being split between `library/model` and `library/internal/epub/edits`. `config` and `edits` both import `book` for validation, keeping the dependency graph clean.
- **Query and Order moved to library/internal/index.** `Query` and `Order` types moved from `library/model` to `library/internal/index` where they are used. Type aliases in the `library` package (`library.Query`, `library.Order`, and the `Order*` constants) also added.
- **Stats moved to library/internal/index.** `Stats` struct moved from `library/model` to `library/internal/index` alongside `Query` and `Order`. Type alias `library.Stats` added for public API access.
- **Author and SeriesRef type aliases moved to library package.** `model.Author` and `model.SeriesRef` type aliases removed from `library/model`. Type aliases `library.Author` and `library.SeriesRef` added to the `library` package for public API access, following the same pattern as `Query`, `Order`, and `Stats`.

## [1.0.0-beta4] - 2026-08-29

### Added

- **ctl id-specs accept search query syntax.** Every id-spec — the first argument to `add-tag`, `remove-tag`, `set-status`, `set-rating`, and `delete` — now takes the same `prefix:value` query language as the search view:
  ```
  add-tag classic author:"Isaac Asimov"+status:read
  set-status reading tag:favorites
  delete series:"Old Trilogy"
  ```
  Title matches in `ctl` are exact (not substring), so a mutating command can only reach the book the operator named. The `help` file documents the full syntax.
- **Query.Order, Query.Limit, Query.ExactTitles.** `Query` gains `Order` (sort by title, date added, date modified, rating, or pubdate), `Limit` (cap result count), and `ExactTitles` (exact match instead of substring, used by `ctl` to prevent accidental edits).
- **store.Update.** A dedicated method for updating a book's files, separate from `Ingest`.
- **HasSeries and SeriesName helpers.** Simplify nil checks on series fields.
- **Calibre sort_title field support.** Can now read and write Calibre's sort_title metadata field.
- **NCX sync for author and title edits.** When editing author or title fields, the NCX (navigation control file) is now synchronized to keep the table of contents consistent.
- **Cover dimensions on edit.** Support for setting cover image dimensions when editing cover metadata.
- **Slot abstraction for etree updates.** Field setters now use a slot abstraction that pushes etree updates out of individual field handlers, making the edit path more uniform.

### Changed

- **SeriesIndex is now a string.** The EPUB 3.3 spec allows decimal-separated levels like `"2.2.1"` for series positions; a float could not represent these. The schema column changed from `REAL` to `TEXT`, and the edit API changed from `*float64` to `*string`. Existing numeric values are preserved as-is.
- **Unified book lookup API.** `Library.Query(model.Filter)` is gone; `Library.Search(model.Query)` is the single API for finding books.
- **view_recent stays sorted.** The `recent/` view now keeps books sorted as they're added or modified, so taking the first N entries is O(N) instead of sorting the whole library on every read.
- **Docker config bind mount.** The `docker run` examples now specify `-v /path/to/config.toml:/etc/ebookfs/config.toml:ro` to bind mount the config file, and no longer suggest `latest` as a real default tag.
- **Epub package rewrite.** The epub parsing and editing layer was rewritten against blackbox tests generated from the EPUB 3.3 and OPF 2.0 specs. This improves spec compliance, fixes edge cases with encoded paths and namespace handling, and makes the edit path more robust.
- **Skip unnecessary epub writes.** The epub is only rewritten if the cover actually changed or the metadata edits change the OPF, avoiding unnecessary file modifications.

### Removed

- **Library.Query(model.Filter).** Replaced by `Library.Search(model.Query)`.
- **Query.Recent.** Replaced by `Query.Order`, which generalises "recent books" to any ordering.
- **epub.Book.** Code uses `model.Bib` directly, eliminating a redundant type.
- **Dead code.** Simplifications across the codebase, including unused filters, redundant conversions, and obsolete comments.

### Fixed

- **sql.ErrNoRows no longer leaks out of the library package.** Internal lookup misses are handled before crossing the library boundary, so callers see clean errors.
- **Removed unneeded in-memory filters from renameTag and renameSeries.** These commands now rely on the database query to return only matching books, avoiding redundant filtering.
- **Cover image detection fixed.** Cover images are now correctly detected and handled during edits.
- **DC prefix for title ensured.** Title elements now properly use the Dublin Core prefix.
- **Duplicate dc:title elements removed.** When setting a title, any other `<dc:title>` elements in the OPF are now removed, preventing duplicate titles.
- **Spec compliance fixes.** Fixed various spec gaps and incorrect/lossy sanitize handling in epub parsing.
- **Encoded rootfile paths.** Fixed handling of URL-encoded rootfile paths in the epub container.
- **File lookup consistency.** Parse and zip operations now handle file lookups the same way, eliminating inconsistencies.
- **Mimetype as first zip entry.** Added validation that mimetype is the first entry in the zip archive, as required by the EPUB spec.
