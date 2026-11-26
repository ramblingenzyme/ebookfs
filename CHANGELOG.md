# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Single-instance lock on the library root.** `Open` takes an exclusive `flock` on `<root>/.lock`. A second process opening the same root fails with `ErrLibraryLocked`, naming the holder as `<hostname>:<pid>`. If the filesystem doesn't enforce locks (a self-test at startup checks), a warning is logged. This prevents the corruption that rolling restarts and misconfigured Compose deploys caused.

- **Filesystem detection at startup.** `Open` calls `statfs` on the library root and refuses to start on SMB/CIFS (magic numbers `0xFF534D42` and `0xFE534D42`), which have unreliable atomic rename and file locking. NFS and FUSE are logged but allowed. The override `WithAllowSMB()`, `--allow-smb` on the binary, permits SMB mounts where the operator understands the risks or detection is wrong. This replaces Grimmory's manual `DISK_TYPE` flag with auto-detection.

- **SQLite journal mode selected by filesystem.** The index detects the filesystem type and chooses the appropriate SQLite journal mode. Local filesystems use WAL for concurrent reads. NFS uses DELETE journal mode for reliability. SMB/CIFS (when permitted via `WithAllowSMB()`) uses DELETE journal mode with EXCLUSIVE locking, a single connection and a 60 second busy timeout, since WAL needs shared memory and a dead client's lock can outlive it. Memory mapping is off on NFS and SMB/CIFS, since an I/O error on a mapped file would kill the process. This replaces the hard-coded WAL mode, which failed on NFS due to shared memory requirements.

- **Additional EPUB metadata fields.** Publisher, rights, subjects, and contributors are now readable and writable through the filesystem interface. Subjects and contributors support multiple values (newline-separated); publisher and rights are single-valued. All four fields are exposed in the OPDS catalog. The database schema version is bumped, so the first startup after upgrading reindexes.

- **OPDS catalog, the second frontend.** An OPDS 1.2 / 2.0 feed at `/opds` on the shared `http.listen` listener, off unless `opds.enable` is set, built on [`github.com/ophymx/opds`](https://github.com/ophymx/opds). Reader apps browse all books, recently added, authors, series, tags and reading status, search over OpenSearch, and download each book's original epub. Read-only: 9P is still the only write path (DECISIONS.md #4). See [docs/security.md](./docs/security.md) before opening the port.

- **kosync, KOReader reading-progress sync.** A kosync-compatible server on the shared HTTP listener at `/sync/`, off unless `kosync.enable` is set. One pre-provisioned account (`kosync.username` and `kosync.password`); registration is refused. Progress lives in each book's `.sidecar/kosync.json`, keyed by KOReader's partial-MD5 document ID, and each sync updates the book's status from the synced percentage using `reading_threshold` and `read_threshold`. A book marked read stays read. The document-ID index is kept in memory and rebuilt from the sidecars at every startup, so a book arriving while kosync is off still gets a document ID on the next restart. See [docs/kosync.md](./docs/kosync.md).

- **`library.state_dir` config for small persistent state.** New `library.state_dir` config field separates the library root (truth) from small persistent state (future queues). Defaults to `library.root`. The index path defaults to `<state_dir>/.index.db`, so the index can be placed on different storage (e.g., local SSD) while state remains with the library.

- **`Library.Authors`, `Library.Series` and `Library.Tags`.** Each returns the distinct values of its field with the number of books behind it, counted in SQL. A frontend building browse navigation no longer loads every book to count them.

- **`epub`, a public package for reading and writing EPUB metadata.** `github.com/ramblingenzyme/ebookfs/pkg/epub` imports nothing of ebookfs. `Open` parses a book's package document into exported fields; `Save` writes back only what moved, leaving the rest of the archive byte for byte. `OpenFile` stops at the zip and OCF container for callers that only need entries. See DECISIONS.md #25.

- **`Book.Format()` and `library.Format`.** Each book reports its file format, comparable against `library.FormatEPUB`. `Format.MediaType()` returns the format's IANA media type, `application/epub+zip` for EPUB. The index records each book's format, so the first startup after upgrading reindexes.

- **`Library.Get(id)`.** Returns one book by id, or an error wrapping `ErrBookNotFound`. `Content`, `Edit` and `Delete` were already id-addressed; reading one book was the gap.
- **`library.Open` takes functional options.** `Open(cfg Config, opts ...Option)` replaces `Open(cfg Config, forceReindex bool)`, with `WithForceReindex()` as the first option. A bare `false` at the call site said nothing, and options are how the planned extension points (ingest hooks, subscribers, metadata handlers) are added without changing the signature again.

- **`identifiers` file in each book directory.** Read-only, one `scheme=value` line per identifier, sorted by scheme. Identifiers were parsed and indexed before but never surfaced anywhere.

- **Ingest hooks.** `library.Hook` is a unified interface for observing and amending library operations. Embed `library.HookBase` and override only the phases you need. Pipeline phases (`PreParse`, `PreCommit`) run during ingest and can abort it; event phases (`OnIngested`, `OnEdited`, `OnDeleting`, `OnDeleted`) fire around edits and deletes. Register with `Library.AddHook(h Hook)`, which calls the hook's `Setup` before registering. Hooks run in registration order; panics are recovered.

- **`opf` command.** `cmd/opf` prints an epub's raw package document (the OPF) to stdout, for piping into `xmllint` or reading directly. It takes one epub path and touches no library or config, building only on `pkg/epub`. A dev/operator tool, not shipped in the Docker image.

### Fixed

- **A zip that is not an epub reports `ErrNotEpub`.** Opening a `.cbz` or other plain zip with `pkg/epub` failed with `ErrContainer`, which pointed at a broken epub.

- **Write-path durability for renames and crash recovery.** Atomic renames now fsync both parent directories before returning, and directories created for them fsync their own parents, preventing a crash from losing a rename the caller was told succeeded. A failed directory fsync is logged rather than returned, since the rename has already happened. This covers epub saves, metadata writes, ingest moves, sidecar writes, and kosync mapping updates. `library.WriteSidecarFile` gives callers already holding a sidecar root the same write as `Library.WriteSidecar`. Temp files left in book directories by an interrupted epub or `meta.toml` write are now cleaned up on startup, instead of only sweeping the inbox temp directory. A crash mid-save previously left these behind forever.

- **Ingest path fsyncs uploaded epubs before rename.** The ingest handle now fsyncs the uploaded epub file before closing it, and the ingest path fsyncs the processed file (original or hook-transformed) before `store.Ingest` renames it to the final location. A failed fsync or close rejects the upload. Without this, a crash after rename but before the file data is flushed could leave the ingested book with incomplete or corrupted data.

- **Identifiers are keyed by scheme rather than by the XML id.** A book indexed `pub-id` and `BookId` where it should have indexed `uuid` and `isbn`. The scheme now comes from `opf:scheme`, the `identifier-type` refinement, or the value's URN namespace, with the XML id as a last resort. The index schema version is bumped, so the first startup after upgrading reindexes and re-derives every identifier row. See DECISIONS.md #24.

- **A repeated tag, author, subject or contributor no longer stops the server starting.** The index refused the repeat, so ingest failed with a raw SQL error, and a repeat already on disk failed the startup rebuild until someone edited the file by hand. Reading an epub or `meta.toml` now keeps the first of each, which also keeps a repeated author out of the book's directory name. An edit that repeats one is refused with a readable error.

- **A hand-edited `meta.toml` with an invalid value no longer stops the server starting.** `rating = nan` failed the startup rebuild for the whole library, while an out-of-range rating, an unknown status or a missing id loaded as-is. Each now gets the checks an edit through ebookfs gets. A book whose sidecar fails them is logged and left out of the index until the file is fixed, as an unparseable `meta.toml` already was.

- **`ctl` reports a total failure as `error:`.** A command where every matched book failed still opened with `ok:`. A partial failure still opens with `ok:`, followed by the errors.

- **Replacing a cover keeps percentage sizes.** An HTML cover page's `<img width="100%">` was rewritten to the new image's pixel width, which overflows a small screen.

- **Sidecar access no longer recreates a deleted book directory.** A sidecar read for a book removed outside ebookfs left an empty `Author/Title (id)/.sidecar/` behind.

### Changed

- **`EpubReader` is now `Reader`, and book paths and sizes drop the "Epub" prefix.** `Library.Content` and the `openFile` argument of `OnIngested`, `OnEdited` and `OnDeleting` return a `library.Reader`, which has no `OPF()` method. `Book.EpubPath()` and `Book.EpubSize()` are now `Path()` and `Size()`. `library.ErrClosed` and `ErrNoCover` are no longer the `pkg/epub` values, so a reader's errors match only the `library` names.

- **One startup lifecycle for every frontend.** `internal/frontend` holds a three-method interface the 9P and OPDS servers implement, and a `Run` that starts them, waits on a context, and shuts them all down against a shared deadline. A frontend that fails to bind now takes the process down instead of being logged while the rest keep serving: a binary answering 9P with its catalog port dead looks healthy to init. `SetupServer` on both frontends is now `New`, taking a package-level `Config` in place of positional parameters, and `Server.Start` is now `Server.Serve`, carrying no listen address, since it blocks and Go reads `Start` as returning immediately. See DECISIONS.md #26.

- **The epub tree no longer knows what an ebookfs book is.** `opf` and `ncx` report what the file says, with nothing rejected or defaulted; ebookfs's own rules moved to the adapter. The refusal to write an unfilable book now runs before the rewrite, so the original survives untouched instead of being replaced and then reported broken. See DECISIONS.md #25.

- **`library/` and `epub/` moved under `pkg/`.** Import paths change: `github.com/ramblingenzyme/ebookfs/library` becomes `.../pkg/library`, and `.../epub` becomes `.../pkg/epub`.
- **`fs/` and its subpackages moved to `internal/fs/`.** Eight packages exporting some 40 constructors and types, with `main.go` as the only caller. A `v1.0.0` tag binds every importable package in the module, and none of these were ever meant to be imported.
- **`library.Library` is a struct, and each frontend package declares the interface it uses.** The interface had one implementation, now the exported `Library` struct returned by `Open`. Adding a library method is additive rather than a change every consumer and fake absorbs.
- **`Reindex` excludes the other mutations.** It moves book directories and rebuilds the index wholesale, neither of which addresses a single book, so the per-book lock could not cover it. `Edit`, `Delete` and ingest now hold a shared lock that `Reindex` takes exclusively.
- **The library owns its config types; the binary's TOML schema moved to `internal/config`.** `library.Config` carries only what the library takes, with no serialization tags. `main.go` maps one to the other, and validation stays at the TOML boundary. The library now depends on no config package, which is what a standalone module needs.
- **`library/model` package removed.** Types previously in `library/model` are now either part of the `library` package or internal:
  - **Public API (now in `library`):** `Book`, `Author`, `Series`, `Edits`, `ValidationError`, `FieldError`, `Query`, `Order`, `Stats`, `EpubReader`
  - **Internal:** `PathSafe` (now in `internal/util/naming`), status constants, `JoinAuthors`, `UnknownAuthor`, `Validate`
  - **`library.Book` is now an immutable wrapper** (`book.ImmutableBook`) with getter methods instead of direct field access:
    - The old struct fields (`book.Title`, `book.Authors`, `book.Meta.Status`, `book.Meta.Rating`, `book.Meta.Tags`, etc.) are now methods (`book.Title()`, `book.Authors()`, `book.Status()`, `book.Rating()`, `book.Tags()`, etc.).
    - Slices and maps returned by getters are cloned to prevent external mutation.
    - `Library.Search` and `Library.Edit` return `*library.Book`.
    - Callers must use the getter methods and re-fetch after mutations (via `Library.Search` or `Library.Edit` return value) to see updated state.

- **`library.inbox_temp` and `library.index_path` default to paths under `library.root`.** They were fixed under `/var/lib/ebookfs/library`, so a config setting only `root` kept the index and upload temp dir there, and startup failed when the two sat on different filesystems. An unset or empty value now follows the configured root. The index path defaults to `<state_dir>/.index.db`, where `state_dir` itself defaults to `library.root`.

- **Unknown config keys log a warning.** A typo such as `enabeld = true` silently left a frontend off. Each key no field reads is logged at startup, and the config still loads.

- **Edits that would leave invalid metadata are refused.** An empty language, which both EPUB versions require, and a series index sent with an empty series, whose index was silently dropped, now fail validation.

- **Ingest refuses a package document over 16 MiB.** A zip entry's declared size is whatever the file says, so a small upload could inflate its package document to gigabytes in memory.

### Removed

- **`library.ErrNotEpub`, `ErrContainer`, `ErrNoRootfile` and `ErrRootfileMissing`.** Ingest now reports a file no format recognizes as `ErrUnknownFormat`, and an epub with a broken container or package document as `ErrInvalidBook`. The error message still names the specific fault.

- **The `reader/` view and kepub conversion.** Use the OPDS catalog to get books onto a device. It serves each book by id, so a metadata edit no longer renames the file a device synced and orphans its reading progress. The `[reader]` config section (`statuses`, `convert`, `cache_dir`) now logs an unknown-key warning at startup, and the kepub cache directory can be deleted. `Library.Exporter` and `library.ReaderConfig` are removed. See ADR-001.

- **`ctl reindex`.** Use `--reindex`, or let startup rebuild when drift detection finds the index and store disagreeing. Run live it moved book directories while the 9P tree kept serving the old names, leaving registry and store disagreeing until restart.

- **The `opf` file in each book's 9P directory.** The raw package document, served live from the epub, is gone from the namespace; run the `opf` command on the epub instead. The OPF's stored uncompressed size is dropped with it. The index schema version is bumped, so the first startup after upgrading reindexes.

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
- **Calibre sort_title field support.** Can now read and write Calibre's sort_title metadata field.
- **NCX sync for author and title edits.** When editing author or title fields, the NCX (navigation control file) is now synchronized to keep the table of contents consistent.
- **Cover dimensions on edit.** Support for setting cover image dimensions when editing cover metadata.

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

### Fixed

- **sql.ErrNoRows no longer leaks out of the library package.** Internal lookup misses are handled before crossing the library boundary, so callers see clean errors.
- **Removed unneeded in-memory filters from renameTag and renameSeries.** These commands now rely on the database query to return only matching books, avoiding redundant filtering.
- **EPUB spec compliance.** Cover detection during edits, the Dublin Core prefix on title elements, removal of duplicate `<dc:title>` when setting a title, URL-encoded rootfile paths in the container, consistent file lookup between parse and zip, and validation that `mimetype` is the first zip entry.
