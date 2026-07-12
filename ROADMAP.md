# ebookfs — Build Plan & Roadmap

## V1 — Current state

### Implemented

- **9P server** serving a synthetic filesystem on TCP.
- **SQLite index** with full CRUD (Query, Get, Put, Delete) and schema versioning.
- **Views:** `books/`, `by-author/`, `by-series/`, `by-tag/`, `by-status/`, `by-id/`, `reader/`, `inbox/`.
- **Per-book directory files:** `book.epub`, `title`, `authors`, `series`, `series_index`, `language`, `description`, `pubdate`, `cover.jpg`, `tags`, `status`, `rating`, `id` — all backed by either the epub's internal OPF or `meta.toml`.
- **Writable metadata:** sidecar fields (status, tags, rating) update `meta.toml` atomically; bib fields (title, authors, series, language, description) rewrite the epub's internal OPF via atomic zip surgery. Cover images are replaceable.
- **Inbox ingestion:** write a file into `inbox/` over 9P; the server parses, validates, allocates an id, and lays the book down atomically. In-flight writes are buffered; `Tclunk` is the transaction boundary.
- **KEPUB conversion:** the `reader/` view can serve Kobo-format renditions via `kepubify/v4` with an on-disk cache and proactive warmer.
- **Structured logging** via `log/slog` with configurable level and format (text or JSON).
- **Reindex on startup:** the index is always rebuilt from the filesystem, guaranteeing no drift between store and cache.

### 9P namespace

```
/
├── books/                       ← flat listing by title
│   └── Title (id)/
│       ├── book.epub            ← read-only epub stream
│       ├── title                ← read/write
│       ├── authors              ← newline-separated, read/write
│       ├── series               ← read/write
│       ├── series_index         ← read/write
│       ├── language             ← read/write
│       ├── description          ← read/write
│       ├── pubdate              ← read-only
│       ├── cover.jpg            ← read/write
│       ├── tags                 ← newline-separated, read/write
│       ├── status               ← read/write
│       ├── rating               ← read/write
│       └── id                   ← read-only
├── by-author/                   ← grouped by first author name
├── by-series/                   ← grouped by series name
├── by-tag/                      ← grouped by tag
├── by-status/                   ← grouped by reading status
│   ├── unread/
│   ├── reading/
│   ├── read/
│   └── abandoned/
├── by-id/                       ← flat listing by id
├── inbox/                       ← write here to ingest
├── reader/                      ← export view for rsync-to-Kobo
│   └── Author/                  ← all authors joined with " & "
│       └── Title.epub           ← or .kepub.epub when Convert
```

### Remaining V1 work

| Feature | Description | Dependencies |
|---------|-------------|--------------|
| Multi-author filename convention | Epub filename and author directory should use all author names joined together (e.g. `Title - Alice & Bob.epub` under a joined author dir) instead of only the first author. Affects `store.path` functions (`epubFilename`, `authorDirName`). The exporter filenames (`epubExporter.Filename`, `kepubCache.Filename`) follow automatically since they're derived from `b.EpubFilename`. The exporter `Dirname` already joins all authors. Existing books must be migrated — the reindex pass can rename directories and files to match the new convention. | None |
| `recent/` view | Shows last 5 books by `date_added` desc. `Filter.Recent` already exists in the query layer; needs a 9P view wired in `server.go`. | None |
| `search/` directory | Virtual root. Walking to `title:foundation/` or `author:asimov/` queries the index and returns matching books. Supported prefixes: `title:`, `author:`, `tag:`, `series:`, `status:`, `id:`. Compound with `/` (e.g. `tag:sci-fi/status:unread`). | Extended query methods (V2 #6) provide the Library-level `Search` and `Count` APIs; this view is a thin 9P wrapper over them. |
| `ctl` file | Plan 9-style control file at the root of the 9P namespace. Write a command line, server parses and executes. Commands: `add-tag <tag> <id-spec>`, `remove-tag <tag> <id-spec>`, `set-status <status> <id-spec>`, `set-rating <0-5> <id-spec>`, `delete <id>`, `reindex`, `rename-tag <old> <new>`, `merge-tags <a> <b>`, `rename-author <old> <new>`, `rename-series <old> <new>`. Reading returns the last result. | Entity management utilities (below) for the last four commands; otherwise none — calls existing Library `Edit`/`Delete`/`Reindex` methods. |
| Entity management utilities | Rename or merge shared entities across every book that references them: tags (rename, merge two into one), authors (update display/sort name — a sort-name change triggers the same directory `Move` an ordinary edit already uses), series (rename, update sort name). Moved here from V2 #11: V1 has no book-subscriber/event system yet, so instead of a bulk index write plus `BookEdited` events, the `ctl` handler resolves affected book ids via `Query` and drives each one through the existing per-book `Edit` path (the same codepath a field-file write already uses) — the registry's live 9P tree stays in sync for free, no new bulk-write path or sync mechanism required. | `ctl` file (delivery mechanism) |
| `stats` file | Read-only at root. Returns formatted text: books, authors, series, tags, total-size, last-added, last-modified. | None (calls `Count`, `ListAuthors`, `ListTags` — part of V2 #6, but could be implemented with SQL queries now). |
| End-to-end test | A fixture library of sample epubs, spinning up `ebookfs` against a temp directory and driving it via real 9P client calls. Exercises the full edit → rewrite path. | None |
| Drift detection: mtime tracking | Startup drift detection currently compares the on-disk store against the index using book directory paths and epub file *size* only. Size alone is unsound: two different epub contents can compress to the identical byte count. `os.Stat` already returns `ModTime()` in the same syscall used for `Size()`, so tracking mtime alongside size is free at check time and closes the collision. While migrating, also add `meta.toml` mtime tracking to catch a sidecar hand-edited outside the 9P mount (status/rating/tags changed directly on the host), which today isn't checked at all. Both need a new index column (`epub_mtime`, `meta_mtime`) — bundle into one schema version bump so there's a single one-time full reindex on next deploy rather than two. | None |

---

## V2 — Standalone Library Module

Refactor `library/` into a standalone Go module with well-defined extension points so third-party code can implement custom frontends, exporters, and import pipelines without forking the project.

### 1. Interface Segregation

Split the single `Library` interface into focused sub-interfaces embedded into a `Library` composite: `BookReader`, `BookIngester`, `BookMutator`, `ExporterSource`, `BookLifecycle`, and `Subscribable`. Every consumer takes only what it needs — `fs/book/file_book.go` accepts `BookReader` (read-only), `fs/inbox/inbox.go` accepts `BookIngester` (write-only), `BookRegistry` accepts `BookMutator` (edit/delete). The composite `Library` satisfies all and is used by the composition root.

### 2. Exporter Registry

Replace the hardcoded `if cfg.Convert { kepub } else { epub }` switch with a global registry where named exporter factories register themselves, mirroring `database/sql`. This includes refactoring the existing `epubExporter` and `kepubCache` to register via `init()` rather than being special-cased in `newExporter()`. `ReaderConfig.Convert` becomes `ReaderConfig.Exporter` (defaults to `"epub"`). `ReaderConfig.CacheDir` is dropped — the kepub exporter stores its converted rendition inside the book's directory via the sidecar file interface (section 5).

Exporter implementations control the *format* of the rendered file (epub vs kepub vs mobi) but do **not** control on-disk naming. The epub filename and author directory conventions (`store.path`) are determined by the library, not by individual exporters. This keeps the directory tree stable regardless of which exporter is configured — a book's on-disk path is the same whether the reader view serves the original epub or a converted kepub.

### 3. Ingest Pipeline Hooks

Add an `IngestHook` interface with `PreProcess` and `PostParse` methods, plus an embeddable `IngestHookBase` with no-op defaults. `PreProcess` receives the staged file path and a per-ingest temp directory (auto-cleaned after the pipeline, regardless of success or failure). The hook returns a reader with converted bytes to avoid intermediate disk I/O, or nil for the fast path. `PostParse` modifies the bibliographic record before commit. Hooks are accepted via functional options on `Open()`.

### 4. Book Event Subscribers

Add a `BookSubscriber` interface (`HandleBookIngested`, `HandleBookEdited`, `HandleBookDeleted`) and a `Subscribable` interface embedded in `Library`. Events fire synchronously after each mutation is committed, under the operation's lock. Subscribers must not block or call back into the library. The existing `fs/registry/BookRegistry` is migrated from holding a `Library` reference to implementing `BookSubscriber` and registering via `lib.Subscribe(reg)`.

### 5. Book Sidecar Files

Add an optional `BookSidecar` interface (`OpenBookFile`, `WriteBookFile`, `RemoveBookFile`, `ListBookFiles`) for reading and writing auxiliary files inside a book's directory. Files are automatically carried along by directory renames and cleaned up on delete. The kepub cache migrates from a separate cache directory to a sidecar file inside each book's directory, eliminating the `CacheDir` config field and its invalidation logic.

### 6. Extended Query Methods

Extend `Filter` with `Search` (LIKE text search), `Offset` (pagination), `SortBy` (field name), and `SortDesc` (direction). Add `Count(Filter)` for `SELECT COUNT(*)` without full hydration, and `ListAuthors`/`ListTags` for browse navigation. These serve query-driven frontends (REST APIs, web UIs, CLIs). The event-driven 9P frontend is unaffected.

### 7. Format-Agnostic Cover Replacement

Currently cover replacement enforces that the new image format matches the existing cover entry's extension (JPEG→JPEG, PNG→PNG). A PNG written to a `cover.jpg` entry is rejected with "a matching format is required (no transcoding)".

**Lift this restriction.** When the format changes, the OPF manifest entry for the cover must be updated to reflect the new filename (`cover.png` instead of `cover.jpg`) and media-type. The `CoverPath` on the book's bib is updated accordingly. The replacement remains otherwise identical — zip surgery, no transcoding.

This is a prerequisite for the cover write path to feel less brittle in frontends that can't control the format their image source produces.

### 8. Metadata Extensibility

`model.Meta` is a fixed struct (Status, Rating, Tags). Plugins and frontends have no standard place to store custom per-book data such as purchase date, read count, personal notes, or shelf location.

**Approach:** Allow registering custom metadata handlers at `Open()` time, via an `OpenOption`. Each handler declares a namespace and provides serialize/deserialize callbacks. Values are stored as sidecar files inside the book directory (e.g. `meta.plugin-name.toml`) and are automatically carried along on move and cleaned up on delete.

```go
type MetaHandler interface {
    Namespace() string                              // e.g. "tracking"
    Serialize(book *model.Book) ([]byte, error)      // TOML or JSON
    Deserialize(book *model.Book, data []byte) error // hydrates custom fields
}
```

This keeps the core `model.Meta` stable while allowing arbitrary extensions. Frontends or plugins that don't need custom metadata are unaffected.

### 8. Module Extraction

Currently `library/` lives inside the `ebookfs` Go module. For third-party frontends to import it as a standalone dependency, it must become its own module with a separate `go.mod`, version tags, and import path.

**Open questions:**
- Should `library/` be extracted to a separate repository (e.g. `github.com/ramblingenzyme/ebookfs-library`) or remain in a subdirectory with its own `go.mod` (monorepo with multi-module workspace)?
- Does the `library/internal/` tree stay internal, or should some packages (epub parsing, store) become public?
- How do versioning and release cadence interact with the main `ebookfs` binary?

**Likely path:** Keep `library/` in the same repository with its own `go.mod` in the `library/` directory. The main module's `go.mod` uses a `replace` directive to point at the local copy during development. This avoids splitting repos while giving `library/` its own version tags.

### 9. Non-goals for V2

- Plugin loader — the registry pattern is compile-time linking only.
- Dynamic config reload — exporters and hooks are set at `Open()` time.
- Distributed / multi-process — subscribers are in-process only.
- Store as public type — capabilities are exposed through targeted Library sub-interfaces.

### 10. Implementation order

Additive library-surface changes first, then internal frontend migrations.

1. Split `Library` into sub-interfaces.
2. Add exporter registry + replace `Convert` config field.
3. Add ingest hooks + `OpenOption`.
4. Add search, count, list methods + extended filter fields.
5. Add book subscriber mechanism.
6. Add sidecar file interface.
7. Add metadata handler mechanism.
8. Decide module extraction approach and split `go.mod`.
9. Migrate `fs/registry/` to subscriber pattern.
10. Migrate `fs/book/` constructors to `BookReader`.
11. Migrate `fs/inbox/` to `BookIngester`.

Steps 1-7 are purely additive to the library surface and independently releasable. Steps 9-11 are internal frontend migrations with no visible change to consumers.

---

### 11. OPDS + HTTP API Frontends

Build two reference frontends on top of the refactored Library module to validate the extension points and demonstrate the patterns for third-party developers.

**OPDS frontend:**
- Serves the library as an OPDS 1.2 / 2.0 catalog (the standard protocol used by ebook reader apps like Moon+ Reader, KyBook, and KOReader).
- Uses `BookReader` for queries and `ExporterSource` for content delivery.
- Navigation (by-author, by-series, by-tag, by-status) maps to OPDS acquisition feeds.
- Search maps to OPDS OpenSearch.
- Download delivers the epub (or converted kepub) via the configured Exporter.

**HTTP API frontend:**
- RESTful JSON API for the library, suitable for web UIs or CLI tools.
- Uses `BookReader` for queries and search, `BookIngester` for adding books, `BookMutator` for edits and deletes, and `BookSidecar` for auxiliary file access.
- Paginated list endpoints with search and filter support.
- Standard CRUD: list, get, create (ingest), update (edit), delete.
- Sidecar file endpoints for thumbnails, covers, and custom metadata.

Both frontends live outside `library/` and depend only on the public Library sub-interfaces, proving that the module can be consumed without access to internal packages. They do not replace the 9P server; they coexist as additional access protocols.
