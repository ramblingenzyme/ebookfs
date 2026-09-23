# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **OPDS catalog, the second frontend.** An OPDS 1.2 / 2.0 feed at `/opds`, off unless `opds.listen` is set, built on [`github.com/ophymx/opds`](https://github.com/ophymx/opds). Reader apps browse all books, recently added, authors, series, tags and reading status, search over OpenSearch, and download through the configured exporter. Covers are served from the original epub, so browsing never triggers a kepub conversion. Read-only: 9P is still the only write path (DECISIONS.md #4). See [docs/security.md](./docs/security.md) before opening the port.

- **`Library.Authors`, `Library.Series` and `Library.Tags`.** Each returns the distinct values of its field with the number of books behind it, counted in SQL. A frontend building browse navigation no longer loads every book to count them.

- **`epub`, a public package for reading and writing EPUB metadata.** `github.com/ramblingenzyme/ebookfs/pkg/epub` imports nothing of ebookfs. `Open` parses a book's package document into exported fields; `Save` writes back only what moved, leaving the rest of the archive byte for byte. `OpenFile` stops at the zip and OCF container for callers that only need entries. See DECISIONS.md #25.

- **`Library.Get(id)`.** Returns one book by id, or an error wrapping `ErrBookNotFound`. `Content`, `Edit` and `Delete` were already id-addressed; reading one book was the gap.
- **`library.Open` takes functional options.** `Open(cfg Config, opts ...Option)` replaces `Open(cfg Config, forceReindex bool)`, with `WithForceReindex()` as the first option. A bare `false` at the call site said nothing, and options are how the planned extension points (ingest hooks, subscribers, metadata handlers) are added without changing the signature again.

- **`identifiers` file in each book directory.** Read-only, one `scheme=value` line per identifier, sorted by scheme. Identifiers were parsed and indexed before but never surfaced anywhere.

### Fixed

- **`ReaderConfig` is validated by the library that owns it.** The rules (a cache dir is required when converting, and it must sit outside the library root or the store walk indexes converted kepubs as books) were enforced only in `internal/config`, so a caller building the struct in Go got neither. `Library.Exporter` now checks both, and the TOML layer no longer repeats them.

- **`Library.Close` no longer converts the whole kepub backlog before returning.** Up to 4096 queued warm hints were converted on the way out, uncancellably. Close now cancels the converter's context and drops the queue. A warm is a hint; the read path still converts on demand.

- **Identifiers are keyed by scheme rather than by the XML id.** A book indexed `pub-id` and `BookId` where it should have indexed `uuid` and `isbn`. The scheme now comes from `opf:scheme`, the `identifier-type` refinement, or the value's URN namespace, with the XML id as a last resort. The index schema version is bumped, so the first startup after upgrading reindexes and re-derives every identifier row. See DECISIONS.md #24.

### Changed

- **One startup lifecycle for every frontend.** `internal/frontend` holds a three-method interface the 9P and OPDS servers implement, and a `Run` that starts them, waits on a context, and shuts them all down against a shared deadline. A frontend that fails to bind now takes the process down instead of being logged while the rest keep serving: a binary answering 9P with its catalog port dead looks healthy to init. `SetupServer` on both frontends is now `New`, taking a package-level `Config` in place of positional parameters, and `Server.Start` is now `Server.Serve`, carrying no listen address, since it blocks and Go reads `Start` as returning immediately. See DECISIONS.md #26.

- **The epub tree no longer knows what an ebookfs book is.** `opf` and `ncx` report what the file says, with nothing rejected or defaulted; ebookfs's own rules moved to the adapter. The refusal to write an unfilable book now runs before the rewrite, so the original survives untouched instead of being replaced and then reported broken. See DECISIONS.md #25.

- **`library/` and `epub/` moved under `pkg/`.** Import paths change: `github.com/ramblingenzyme/ebookfs/library` becomes `.../pkg/library`, and `.../epub` becomes `.../pkg/epub`. The Dockerfile moved to `build/`, `config.example.toml` to `configs/`, and the shared test packages under `internal/testing/`.
- **`fs/` and its subpackages moved to `internal/fs/`.** Eight packages exporting some 40 constructors and types, with `main.go` as the only caller. A `v1.0.0` tag binds every importable package in the module, and none of these were ever meant to be imported.
- **`library.Library` is a struct, and each frontend package declares the interface it uses.** The interface had one implementation, now the exported `Library` struct returned by `Open`. Adding a library method is additive rather than a change every consumer and fake absorbs.
- **`Reindex` excludes the other mutations.** It moves book directories and rebuilds the index wholesale, neither of which addresses a single book, so the per-book lock could not cover it. `Edit`, `Delete` and ingest now hold a shared lock that `Reindex` takes exclusively.
- **The library owns its config types; the binary's TOML schema moved to `internal/config`.** `library.Config` and `library.ReaderConfig` carry only what the library takes, with no serialization tags. `main.go` maps one to the other, and validation stays at the TOML boundary. The library now depends on no config package, which is what a standalone module needs.
- **`library/model` package removed.** Types previously in `library/model` are now either part of the `library` package or internal:
  - **Public API (now in `library`):** `Book`, `Author`, `Series`, `Edits`, `ValidationError`, `FieldError`, `Query`, `Order`, `Stats`, `EpubReader`
  - **Internal:** `PathSafe` (now in `internal/naming`), status constants, `JoinAuthors`, `UnknownAuthor`, `Validate`
  - **`library.Book` is now an immutable wrapper** (`book.ImmutableBook`) with getter methods instead of direct field access:
    - The old struct fields (`book.Title`, `book.Authors`, `book.Meta.Status`, `book.Meta.Rating`, `book.Meta.Tags`, etc.) are now methods (`book.Title()`, `book.Authors()`, `book.Status()`, `book.Rating()`, `book.Tags()`, etc.).
    - Slices and maps returned by getters are cloned to prevent external mutation.
    - `Library.Search`, `Library.Edit`, and all `Exporter` methods return or accept `*library.Book`.
    - Callers must use the getter methods and re-fetch after mutations (via `Library.Search` or `Library.Edit` return value) to see updated state.

### Removed

- **`ctl reindex`.** Use `--reindex`, or let startup rebuild when drift detection finds the index and store disagreeing. Run live it moved book directories while the 9P tree kept serving the old names, leaving registry and store disagreeing until restart.

## [1.0.0-beta4] - 2026-08-29

### Added

- **ctl id-specs accept search query syntax.** Every id-spec, the first argument to `add-tag`, `remove-tag`, `set-status`, `set-rating` and `delete`, now takes the same `prefix:value` query language as the search view:
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
- **EPUB spec compliance.** Cover detection during edits, the Dublin Core prefix on title elements, removal of duplicate `<dc:title>` when setting a title, URL-encoded rootfile paths in the container, consistent file lookup between parse and zip, and validation that `mimetype` is the first zip entry.
