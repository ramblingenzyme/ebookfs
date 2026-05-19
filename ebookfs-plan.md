# ebookfs & ebooktui — Build Plan

A self-hosted ebook library system with no Calibre, no cloud, no Google Drive. The Pi runs `ebookfs` (9P server + SQLite index over a real filesystem). The Optiplex runs `ebooktui` (tview-based TUI mounting the 9P share). The Kobo gets books via USB sideload from the Optiplex.

## Design principles

1. **Filesystem is the source of truth.** Books live as real files in a clean directory tree on the Pi. The SQLite index is a derived cache — rebuildable from the filesystem alone.
2. **Per-book sidecars carry the bits OPF can't.** OPF inside the epub holds canonical bibliographic data. A `meta.toml` sidecar carries reading status, custom tags, internal id, and anything outside OPF's scope.
3. **9P is the only network protocol.** No HTTP, no gRPC, no custom RPC. Reads, writes, and control flow all happen through filesystem operations on the synthetic namespace.
4. **The TUI is a 9P client and nothing more.** All "operations" on books are file ops on the mount. This keeps the contract between client and server minimal and language-agnostic — anything that can mount 9P can drive the library.
5. **Single Go binary per side.** Both `ebookfs` and `ebooktui` ship as one binary each, built with `CGO_ENABLED=0` for clean cross-compile to ARM.

## Hardware

- **Pi:** Raspberry Pi 3, 1GB RAM. Library lives on whatever storage the Pi has (SD card or USB drive — recommend USB for IO and longevity).
- **Optiplex:** Dell Optiplex 9020 USFF running Linux. Mounts the Pi's 9P export at `/mnt/library`. Has Kobo connected occasionally over USB.
- **NAS:** Off-the-shelf, runs `restic` (or similar) pulling from the Pi on a schedule.

---

# Part 1 — `ebookfs`

The 9P server and library indexer running on the Pi.

## Storage layout

Calibre-compatible directory structure. This is a deliberate concession: the layout is a stable de-facto standard, and adopting it keeps the door open to running another tool against the same library if `ebookfs` ever gets retired.

```
library/
├── Le Guin, Ursula K/
│   └── The Left Hand of Darkness (1042)/
│       ├── The Left Hand of Darkness - Ursula K. Le Guin.epub
│       └── meta.toml               ← ebookfs-specific sidecar
├── Asimov, Isaac/
│   └── Foundation (47)/
│       └── ...
└── .index.db                       ← derived SQLite cache

# Note: inbox/ is NOT a real directory. It exists only as a synthetic
# node in the 9P namespace. See "9P namespace > Inbox" below.
```

Author directories use `Surname, Forename` form (the OPF `file-as` convention). Book directories are `Title (id)`. The `(id)` is the integer primary key from the SQLite index — it gives every book a stable, short identifier that survives renames.

## `meta.toml` sidecar schema

Stored at `library/<author>/<book-dir>/meta.toml`. Pure TOML, hand-editable, version-controlled.

```toml
id = 1042
date_added = "2026-05-07T14:23:00Z"
date_modified = "2026-05-07T14:23:00Z"
status = "unread"            # unread | reading | read | abandoned
rating = 0                   # 0-5, 0 = unrated
custom_tags = ["sci-fi", "feminist", "classic"]

[reading]
last_position = ""           # opaque epub CFI when populated by Kobo sync
last_read = ""               # ISO 8601

[notes]
text = ""
```

**Bibliographic metadata (title, authors, series, identifiers, language, pubdate, description) lives inside the epub's own OPF — `OEBPS/content.opf` within the zip.** This is the canonical source. The Kobo, every reader app, and every other tool reads metadata from there. There is no sidecar OPF — it would just duplicate what the epub already contains, and the two would inevitably drift.

`meta.toml` carries only the things that don't belong in OPF: our internal id (used for the `(id)` directory suffix), reading status, rating, ebookfs-specific custom tags, and reading position. Editing the title or author of a book means rewriting the epub's internal OPF, not editing a sidecar.

## SQLite index

Lives at `library/.index.db` — never accessed over 9P. Pure cache. `ebookfs reindex` rebuilds from scratch.

Use `modernc.org/sqlite` (pure Go) so the Pi binary cross-compiles without cgo.

```sql
-- Schema v1

CREATE TABLE books (
    id              INTEGER PRIMARY KEY,
    title           TEXT NOT NULL,
    sort_title      TEXT,
    series_id       INTEGER REFERENCES series(id) ON DELETE SET NULL,
    series_index    REAL,
    pubdate         TEXT,                    -- ISO 8601, may be partial
    description     TEXT,
    language        TEXT,
    library_path    TEXT NOT NULL UNIQUE,    -- relative to library root
    epub_filename   TEXT NOT NULL,
    has_cover       INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'unread',
    rating          INTEGER NOT NULL DEFAULT 0,
    date_added      TEXT NOT NULL,
    date_modified   TEXT NOT NULL
);

CREATE TABLE authors (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,        -- display form: "Ursula K. Le Guin"
    sort_name   TEXT NOT NULL                -- file-as form: "Le Guin, Ursula K"
);

CREATE TABLE book_authors (
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    author_id   INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'aut', -- aut, edt, ill, trl
    position    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (book_id, author_id, role)
);

CREATE TABLE series (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    sort_name   TEXT
);

CREATE TABLE tags (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE
);

CREATE TABLE book_tags (
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    tag_id      INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, tag_id)
);

CREATE TABLE identifiers (
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    scheme      TEXT NOT NULL,               -- isbn, uuid, openlibrary, etc.
    value       TEXT NOT NULL,
    PRIMARY KEY (book_id, scheme)
);

CREATE VIRTUAL TABLE books_fts USING fts5(
    title, description,
    content=books, content_rowid=id,
    tokenize='porter unicode61'
);

CREATE INDEX idx_books_status      ON books(status);
CREATE INDEX idx_books_pubdate     ON books(pubdate);
CREATE INDEX idx_books_date_added  ON books(date_added);
CREATE INDEX idx_authors_sort      ON authors(sort_name);
```

Schema versioning lives in a `pragma user_version`. Migrations are append-only — never edit a past migration.

## 9P namespace

The synthetic filesystem `ebookfs` exposes. This is the contract.

```
/
├── by-author/
│   └── Le Guin, Ursula K/
│       └── The Left Hand of Darkness (1042)/
│           ├── book.epub        ← real file, read-only stream
│           ├── metadata         ← TOML, read/write — front-end for meta.toml
│           ├── opf              ← raw OPF XML extracted from the epub, read-only
│           ├── cover.jpg        ← read/write
│           ├── tags             ← newline-separated, read/write
│           ├── status           ← single-line, read/write
│           ├── rating           ← single integer, read/write
│           └── id               ← read-only "1042"
├── by-series/
│   └── Foundation/
│       ├── 1 - Foundation (47).epub
│       ├── 2 - Foundation and Empire (48).epub
│       └── 3 - Second Foundation (49).epub
├── by-tag/
│   └── sci-fi/
│       └── (book directories, like by-author)
├── by-status/
│   ├── unread/
│   ├── reading/
│   ├── read/
│   └── abandoned/
├── by-id/
│   └── 1042/                    ← same per-book directory shape as above
├── recent/                      ← last 50 added, by date_added desc
├── search/
│   ├── title:foundation/        ← walk to a synthetic dir, get matches
│   ├── author:asimov/
│   └── fts:robot+laws/
├── inbox/                       ← write here to ingest
├── ctl                          ← command file for batch ops
└── stats                        ← read-only library statistics
```

### Per-book directory file semantics

The files in a per-book directory split into two categories:

- **OPF-backed** (lives in the epub): `metadata`, `opf`, `cover.jpg`. Writes to these rewrite the epub via `ebook-meta` (see *Write path* below).
- **Sidecar-backed** (lives in `meta.toml`): `tags`, `status`, `rating`. Writes update `meta.toml` and the SQLite index.

| File | Backing | Mode | Read returns | Write effect |
|---|---|---|---|---|
| `book.epub` | epub | `0444` | streams the actual epub bytes | (denied) |
| `metadata` | epub | `0644` | OPF data projected as TOML (title, authors, series, language, pubdate, description, identifiers) | parses TOML, invokes `ebook-meta` to rewrite the epub's internal OPF |
| `opf` | epub | `0444` | raw OPF XML extracted from the epub | (denied) |
| `cover.jpg` | epub | `0644` | extracted cover image bytes | invokes `ebook-meta --cover` to embed new cover into the epub |
| `tags` | sidecar | `0644` | newline-separated tag list | updates `meta.toml` and index |
| `status` | sidecar | `0644` | single line, e.g. `reading\n` | validates against enum, updates `meta.toml` and index |
| `rating` | sidecar | `0644` | single integer 0-5 | validates, updates `meta.toml` and index |
| `id` | sidecar | `0444` | integer | (denied) |

**Write transactionality:** writes are buffered server-side and flushed on `Tclunk`. Partial writes don't corrupt state. If a write fails validation (e.g. invalid status, malformed TOML, `ebook-meta` returns nonzero), `Tclunk` returns an error and the buffered content is discarded. For epub-backed writes, `ebook-meta` runs against a temp copy and atomic-renames over the canonical file on success — so a failed rewrite leaves the original intact.

### Search directories

`search/` is a virtual root. Walking to a name like `author:asimov` triggers a query parse. Reading the directory returns matching book entries (as symlinks back into `by-id/` or as the per-book directories themselves — implementation choice).

Supported query prefixes:

- `title:<term>`
- `author:<term>`
- `tag:<tag>`
- `series:<name>`
- `status:<status>`
- `fts:<terms>` — full-text search via FTS5
- `id:<n>` — direct id lookup

Compound queries via `+` joining: `tag:sci-fi+status:unread`.

### `ctl` file

Plan 9-style control file. Write a command line, server parses and executes. Reading returns a summary of the last command's result.

Commands:

```
add-tag <tag> <id-spec>            # id-spec: id:1 or id:1,2,3 or id:1..200
remove-tag <tag> <id-spec>
set-status <status> <id-spec>
set-rating <0-5> <id-spec>
move <id> <new-author> [new-title] # rebuilds path, updates index
delete <id>                         # removes from library and index
reindex                             # full rebuild from filesystem
```

Example session:

```
echo 'add-tag sci-fi id:1042,1043,1044' > ctl
cat ctl  # => "ok: 3 books updated"
```

### Inbox

`inbox/` is **purely synthetic** — there is no real `inbox/` directory on disk. It exists only as a node in the 9P namespace.

Ingestion happens entirely through 9P file operations:

1. Client `Tcreate inbox/<filename>`. Server allocates a temp file at `/var/lib/ebookfs/inbox-tmp/<random>.epub` and associates it with the 9P fid.
2. Client `Twrite`s bytes. Server appends to the temp file. Streaming — no memory pressure even for large epubs.
3. Client `Tclunk`s. Server closes the temp file and runs the ingestion pipeline:
   - Parse epub (zip open, container.xml, OPF).
   - Validate (must be a real epub, must have at minimum a title and one creator).
   - Allocate id from SQLite, compute canonical path.
   - `rename(2)` temp file into canonical location (works because temp dir is on the same filesystem as `library/`).
   - Insert SQLite rows, write `meta.toml`.
   - On any failure: delete temp, return descriptive error from `Tclunk`. The client (TUI, or a `cp` command) sees the error directly.

Listing `inbox/` returns empty by default. Successful ingestions transit through invisibly — files don't pile up.

This design means the server never has to detect "is the file done writing yet?" — `Tclunk` is the explicit transaction boundary. No fsnotify, no race conditions on partial writes, no `.failed` files lingering on disk.

**External tools** (LazyLibrarian, scripts, cron jobs) drop files into `inbox/` by writing to a 9P mount. The Pi mounts its own 9P export locally over loopback (see *Local mount on the Pi* in Deployment), so any process on the Pi can `cp foo.epub /mnt/library/inbox/` and ingestion happens transparently.

### `stats` file

Read-only. Returns formatted text:

```
books: 487
authors: 312
series: 28
tags: 47
total-size: 1.3 GB
last-added: 2026-05-06 14:22
last-modified: 2026-05-07 09:11
```

## Module layout

```
ebookfs/
├── cmd/
│   └── ebookfs/
│       └── main.go                 # config loading, signal handling, wire up subsystems
├── internal/
│   ├── config/                     # TOML config parsing
│   ├── epub/                       # epub I/O
│   │   ├── parse.go                # hand-written: zip + OPF parser, ~200 lines
│   │   ├── cover.go                # extract cover bytes from an epub
│   │   └── write.go                # subprocess wrapper around `ebook-meta`
│   ├── store/                      # filesystem operations on the library tree
│   │   ├── store.go                # high-level Library type
│   │   ├── ingest.go               # inbox -> canonical path
│   │   ├── path.go                 # safe path construction (sanitise filenames)
│   │   └── meta.go                 # meta.toml read/write
│   ├── index/                      # SQLite-backed search/lookup
│   │   ├── schema.sql              # embedded
│   │   ├── migrate.go
│   │   ├── books.go                # book CRUD
│   │   ├── search.go               # query parser + executor
│   │   └── reindex.go              # full rebuild from filesystem
│   ├── ninep/                      # 9P server
│   │   ├── server.go               # main 9P loop, transport setup
│   │   ├── fs.go                   # FileSystem interface implementation
│   │   ├── nodes/                  # node types
│   │   │   ├── byauthor.go
│   │   │   ├── bytag.go
│   │   │   ├── byid.go
│   │   │   ├── book.go             # per-book directory
│   │   │   ├── search.go
│   │   │   ├── ctl.go
│   │   │   ├── inbox.go            # synthetic dir; Tcreate/Twrite/Tclunk → ingest
│   │   │   └── stats.go
│   │   └── ops.go                  # buffered write semantics, validation
├── go.mod
└── go.sum
```

## Library choices

| Concern | Pick | Why |
|---|---|---|
| 9P implementation | `github.com/hugelgupf/p9` | Modern, 9P2000.L, well maintained, used in production |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no cgo, clean ARM cross-compile |
| Config | `github.com/BurntSushi/toml` | TOML stdlib equivalent |
| Logging | `log/slog` (stdlib) | Built in since 1.21 |
| Testing | stdlib + `testing/fstest` | No need for a framework |
| epub writes | `ebook-meta` (Calibre CLI, runtime dep) | The Go ebook ecosystem has no maintained library for writing into existing epubs. `ebook-meta` is Calibre's single-purpose CLI for exactly this and has 15+ years of edge-case handling. Invoked as a subprocess; no Calibre daemon runs. |

The epub *parser* is hand-written (~200 lines: zip open, find container.xml, parse OPF) — the stated goal of understanding the format applies to reads. *Writes* delegate to `ebook-meta`. The asymmetry is deliberate: zip + OPF parsing is small and well-defined, but a robust epub writer has to handle ZIP64, malformed mimetype entries, ADEPT-encrypted files, EPUB 2 vs 3 schema differences, and vendor-specific OPF extensions — too much surface area to hand-roll responsibly when a battle-tested CLI exists.

## Configuration

`/etc/ebookfs/config.toml`:

```toml
[library]
root = "/mnt/storage/library"
inbox_temp = "/mnt/storage/library/.inbox-tmp"   # buffer dir for in-flight 9P
                                                  # writes; must be on same
                                                  # filesystem as `root` so
                                                  # the final move is rename(2)

[index]
path = "/mnt/storage/library/.index.db"

[epub]
ebook_meta = "/usr/bin/ebook-meta"   # path to Calibre's ebook-meta CLI
                                      # ebookfs refuses to start if missing

[server]
listen = "tcp!0.0.0.0!5640"          # 9P standard port
auth = "none"                         # "none" | "shared-secret"
shared_secret_file = ""               # path if auth = shared-secret

[log]
level = "info"
format = "text"                       # "text" | "json"
```

## Deployment

### Prerequisites on the Pi

```
apt install --no-install-recommends calibre-bin
```

This pulls in Calibre's CLI tools (`ebook-meta`, `ebook-convert`, etc.) without the GUI stack. Roughly 80MB on disk. The Calibre application is never invoked; only `ebook-meta` runs, as a short-lived subprocess on each epub write.

`ebookfs` checks for `ebook-meta` on startup (path from config) and refuses to start if it's missing or non-executable. Failing fast at boot is much better than failing at first metadata edit.

### systemd unit

systemd unit at `/etc/systemd/system/ebookfs.service`:

```ini
[Unit]
Description=ebookfs - 9P ebook library server
After=network.target

[Service]
Type=simple
User=ebookfs
ExecStart=/usr/local/bin/ebookfs --config /etc/ebookfs/config.toml
Restart=on-failure
RestartSec=5

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/mnt/storage/library
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

### Local mount on the Pi

The Pi mounts its own 9P export over loopback so any process running on the Pi can interact with the library through ordinary filesystem operations. This is what makes external tools like LazyLibrarian able to drop files into `inbox/` without speaking 9P themselves.

systemd mount unit at `/etc/systemd/system/mnt-ebookfs.mount`:

```ini
[Unit]
Description=Local 9P mount of ebookfs
Requires=ebookfs.service
After=ebookfs.service

[Mount]
What=127.0.0.1
Where=/mnt/ebookfs
Type=9p
Options=trans=tcp,port=5640,version=9p2000.L,uname=root,_netdev

[Install]
WantedBy=multi-user.target
```

`Requires=` + `After=` ensures the mount only attempts after `ebookfs` is up. If the server restarts, the mount stays, and clients see read errors briefly until it's back — fine for the LazyLibrarian use case.

LazyLibrarian's post-processing destination becomes `/mnt/ebookfs/inbox/`. A `cp` into that directory translates to `Tcreate`+`Twrite`+`Tclunk` on the loopback connection, triggering the same ingestion pipeline as a remote write from the Optiplex TUI.

The architectural property worth naming: 9P is the only ingestion path, full stop. Local and remote tools both go through it. There's no second code path for "local files" vs "network files."

## Key flows

### Cold start

1. Load config.
2. Verify `ebook-meta` is present at the configured path; refuse to start if missing.
3. Ensure `inbox_temp` directory exists; clean any stale temp files left by previous crashed writes.
4. Open SQLite index. Check schema version. Run migrations if needed.
5. Quick sanity check: count rows in `books` vs count of book-directories in `library/`. If they differ by more than a small threshold, log a warning and offer `--reindex` hint. Don't auto-reindex without an explicit flag.
6. Bind 9P listener, serve.

### Inbox ingestion via 9P

1. Client (TUI on Optiplex, or `cp` on Pi via loopback mount) issues `Tcreate inbox/<filename>`. Server allocates a temp file at `<inbox_temp>/<random>.epub` and associates it with the 9P fid.
2. Client streams bytes via `Twrite`. Server appends to the temp file.
3. Client issues `Tclunk`. Server begins ingestion.
4. Parse epub: open zip, find container.xml, parse OPF. On parse failure: delete temp, return descriptive error from `Tclunk`.
5. Validate: must have at minimum a title and one creator. On validation failure: delete temp, return error.
6. Begin SQLite transaction. Insert book row, get auto-incremented id.
7. Construct canonical path `library/<file-as>/<title> (id)/<title> - <author>.epub`. Sanitise for filesystem-illegal characters.
8. `rename(2)` temp into canonical location (works because temp dir is on the same filesystem).
9. Write `meta.toml` (id + defaults for status/rating + empty custom_tags + `date_added`/`date_modified`).
10. Commit transaction.
11. Return success on `Tclunk`.

If anything between step 4 and step 10 fails: rollback transaction, delete temp, return descriptive error. The client (TUI or shell command) sees the error directly. No `.failed` files litter the disk.

### Sidecar write via 9P (status, tags, rating)

1. Client `Topen` on `/by-id/1042/status`, `Twrite` `"reading\n"`, `Tclunk`.
2. On `Tclunk`, server validates content against status enum.
3. Begin SQLite transaction. Update `books.status`. Rewrite `meta.toml` on disk. Commit.
4. Return success. If validation or disk write fails, return error from `Tclunk` and discard buffered state.

### OPF write via 9P (title, authors, series, etc.)

1. Client `Topen` on `/by-id/1042/metadata`, `Twrite` with new TOML, `Tclunk`.
2. On `Tclunk`, server parses the TOML. If parse fails, error and stop.
3. Server copies `book.epub` to `book.epub.tmp` in the same directory.
4. Server invokes `ebook-meta book.epub.tmp --title="..." --authors="..." [...]` with flags derived from the parsed TOML. Captures stdout/stderr.
5. If `ebook-meta` exits non-zero, delete temp, return error from `Tclunk`.
6. Open the temp file as a zip; verify it parses and that container.xml + OPF are still readable. If not, delete temp, return error.
7. `fsync` temp, then atomic `rename(2)` over `book.epub`. The original is replaced atomically; readers either see old or new, never partial.
8. Update SQLite index from the new epub's OPF in the same transaction as `meta.toml` `date_modified`. Commit.
9. Return success.

### Cover write via 9P

Same as OPF write but `ebook-meta book.epub.tmp --cover=<temp-image-file>`. Image bytes are buffered in memory (or to a temp file if large), passed by path.

### Reindex

1. Walk `library/` looking for `meta.toml` files.
2. For each, parse the adjacent epub's internal OPF directly (zip open + container.xml + content.opf).
3. Truncate `books`, `authors`, `book_authors`, `tags`, `book_tags`, `series`, `identifiers`, `books_fts`.
4. Bulk-insert from epub OPF (bibliographic) + meta.toml (sidecar fields).
5. The `id` field in `meta.toml` is preserved — that's the whole point of having it. Reindex doesn't allocate new ids unless a `meta.toml` is missing one (in which case allocate from `MAX(id)+1`).
6. **Id collisions during reindex are fatal.** If two `meta.toml` files claim the same id, abort reindex and surface the conflict — never silently renumber. The user fixes it manually (typically by deleting one of the two `meta.toml` files, since the duplicate almost always means a restored backup or a botched manual copy).

## Testing strategy

- **Unit tests** for epub parser, path sanitisation, query parser, meta.toml serialisation.
- **Integration tests** for the 9P server using a fake transport (in-process). Mount the FS via the library's test harness and exercise read/write/walk.
- **End-to-end test** with a fixture library of 10 sample epubs (public domain, e.g. from Project Gutenberg), spinning up `ebookfs` against a temp directory and driving it via real 9P client calls. The end-to-end suite requires `ebook-meta` on `$PATH` and exercises the full edit→rewrite path against real epubs.
- **`ebook-meta` wrapper tests** verify subprocess argument construction, exit-code handling, and that a failed `ebook-meta` invocation leaves the original epub untouched (atomic rename invariant).
- **No mocking of SQLite.** Use real SQLite against `:memory:` for index tests.

## Out of scope for v1

- HTTP server / Kobo browser interface (decided against — see chat history).
- Drive sync (decided against).
- DRM removal.
- Online metadata fetching (OpenLibrary/Google Books). Manual import only.
- Multi-user / auth more complex than shared secret.
- Reading progress sync from Kobo. Hooks designed for it (see `meta.toml [reading]` section), implementation deferred.
- Cover art generation for books that lack one.

---

# Part 2 — `ebooktui`

The tview-based TUI on the Optiplex.

## Architecture

```
┌──────────────────────────────────────┐
│  Optiplex                            │
│                                      │
│  ┌────────────────┐                  │
│  │ ebooktui       │                  │
│  │ (tview + Go)   │                  │
│  └────────┬───────┘                  │
│           │ filesystem ops           │
│  ┌────────▼─────────────┐            │
│  │ /mnt/library         │            │
│  │ (v9fs, mount.9p)     │            │
│  └──────────────────────┘            │
│           │                          │
│           │ 9P/TCP                   │
└───────────┼──────────────────────────┘
            │
┌───────────▼──────────────────────────┐
│  Pi: ebookfs (port 5640)             │
└──────────────────────────────────────┘

┌──────────────────────────────────────┐
│  Optiplex (when Kobo connected)      │
│                                      │
│  ┌──────────────┐                    │
│  │ ebooktui     │  detects USB,      │
│  │              │  enables sideload  │
│  └──────┬───────┘                    │
│         │ file copy                  │
│  ┌──────▼──────────────┐             │
│  │ /run/media/.../KOBO │             │
│  └─────────────────────┘             │
└──────────────────────────────────────┘
```

The TUI does not speak 9P directly. The 9P mount is set up at the OS level via `mount.9p` (kernel `v9fs` module) at `/mnt/library`. The TUI does ordinary file I/O against that path. This means the TUI is unit-testable against any real directory — drop a fake library at `/tmp/fake-library` and point the binary at it.

## Layout

```
┌──────────────────────────────────────────────────────┐
│ ebooktui  │  487 books │ Pi connected │ Kobo: idle   │ ← header
├──────────────────────────────────────────────────────┤
│ Authors    │ Books                                   │
│            │                                         │
│ > Asimov   │ Foundation              [unread]        │
│   Le Guin  │ Foundation and Empire   [read]          │
│   Tolkien  │ Second Foundation       [reading]       │
│   ...      │ Foundation's Edge       [unread]        │
│            │ ...                                     │
│            │                                         │
│            ├─────────────────────────────────────────┤
│            │ Title:    Foundation                    │
│            │ Author:   Isaac Asimov                  │
│            │ Series:   Foundation #1                 │
│            │ Tags:     sci-fi, classic, golden-age   │
│            │ Status:   unread                        │
│            │ Added:    2026-04-12                    │
│            │ ISBN:     978-0553293357                │
│            │                                         │
├──────────────────────────────────────────────────────┤
│ /:search  e:edit  t:tag  s:status  k:send-to-kobo   │ ← footer
└──────────────────────────────────────────────────────┘
```

Three main panels managed by a `tview.Flex`:

- **Left:** scope selector (Authors / Series / Tags / Status / Recent / Search). `tview.List`.
- **Middle:** book list within the selected scope. `tview.Table` with sortable columns.
- **Right (top):** detail pane for the selected book. `tview.TextView`.
- **Right (bottom)** *(stretch)*: cover art, if the terminal supports sixel/kitty graphics. Otherwise an ASCII placeholder.

`tview.Pages` overlays for modals (edit metadata form, sideload progress, confirmation dialogs).

## Key bindings

| Key | Action |
|---|---|
| `Tab` / `Shift-Tab` | Cycle focus between panels |
| `j` / `k` or arrows | Move within the focused list/table |
| `Enter` | Select / open |
| `/` | Begin search (full-text via `search/fts:...` on 9P) |
| `e` | Edit metadata in `$EDITOR` |
| `t` | Add/remove tags (modal) |
| `s` | Set reading status (cycle: unread → reading → read → abandoned) |
| `r` | Set rating (modal, 0-5) |
| `k` | Send selected book(s) to Kobo (only if Kobo present) |
| `i` | Open inbox view (drag-and-drop or path-paste to import) |
| `:` | Command palette (raw `ctl` commands) |
| `?` | Help overlay |
| `Ctrl-R` | Refresh from server |
| `Ctrl-Q` | Quit |

Multi-select with `Space` in book list, then operations apply to the selection.

## Module layout

```
ebooktui/
├── cmd/
│   └── ebooktui/
│       └── main.go
├── internal/
│   ├── config/                     # TOML config
│   ├── library/                    # client over the 9P mount
│   │   ├── library.go              # high-level Library type wrapping the mount
│   │   ├── books.go                # listing, lookup
│   │   ├── search.go               # writes to search/<query>/, reads back
│   │   ├── ctl.go                  # ctl command dispatcher
│   │   ├── meta.go                 # read/write metadata via TOML file
│   │   └── watch.go                # poll the mount for changes (v9fs doesn't propagate inotify events)
│   ├── kobo/
│   │   ├── detect.go               # find KOBO mount via /proc/mounts
│   │   ├── sideload.go             # copy book.epub to KOBO/
│   │   └── filename.go             # filename rules for Kobo (id-prefixed)
│   ├── ui/
│   │   ├── app.go                  # tview.Application, root layout
│   │   ├── header.go
│   │   ├── footer.go
│   │   ├── scopes.go               # left panel
│   │   ├── booklist.go             # middle panel
│   │   ├── details.go              # right panel
│   │   ├── modals/
│   │   │   ├── tags.go
│   │   │   ├── rating.go
│   │   │   ├── search.go
│   │   │   └── confirm.go
│   │   └── async.go                # QueueUpdateDraw helpers
│   └── editor/
│       └── editor.go               # app.Suspend + $EDITOR shell-out
├── go.mod
└── go.sum
```

## 9P mount setup

Done at the OS level, not by the TUI. Add to `/etc/fstab`:

```
pi.local 5640  /mnt/library  9p  trans=tcp,version=9p2000.L,uname=ebooktui,_netdev,nofail  0  0
```

Or invoke manually:

```
mount -t 9p -o trans=tcp,port=5640,version=9p2000.L pi.local /mnt/library
```

The `nofail` and `_netdev` options mean the Optiplex still boots if the Pi is unreachable. `ebooktui` should detect a missing or stale mount on startup and display an error rather than crash.

## Async I/O pattern

All 9P-backed operations (every directory listing, every metadata read, every search) potentially block on a network round-trip. tview's event handlers run on the main goroutine; blocking them freezes the UI.

Pattern, applied universally:

```go
func (a *App) loadBooks(scope string) {
    a.setStatus("loading...")
    go func() {
        books, err := a.lib.ListByScope(scope)
        a.app.QueueUpdateDraw(func() {
            if err != nil {
                a.setStatus("error: " + err.Error())
                return
            }
            a.bookList.Update(books)
            a.setStatus("")
        })
    }()
}
```

A small `internal/ui/async.go` helper standardises this so the pattern doesn't drift across handlers.

## Kobo USB detection

Polling `/proc/mounts` on a 2-second tick is sufficient. Look for a mount with `LABEL=KOBOeReader` or device path matching the Kobo USB vendor id (`0x2237`).

When detected:

1. Header shows `Kobo: connected`.
2. The `k` keybinding becomes active.
3. Optional auto-action: read `KoboReader.sqlite` and surface "books on device" stats. (Deferred to v2.)

When a Kobo is mounted, `ebooktui` does not write to it without explicit user action. Sideload happens only when the user presses `k` on a selection.

### Sideload procedure

For each selected book:

1. Construct destination filename: `<id> - <Title> - <Author>.epub`. Id-prefix gives a stable handle for the eventual progress-sync feature.
2. Copy `/mnt/library/by-id/<id>/book.epub` to `<kobo-mount>/<destination>`.
3. Update progress modal with per-file status.
4. On completion, optionally `sync` the Kobo mount and prompt for ejection.

## Configuration

`~/.config/ebooktui/config.toml`:

```toml
[library]
mount = "/mnt/library"

[editor]
command = ""                # falls back to $EDITOR or $VISUAL

[kobo]
poll_interval = "2s"
mount_label = "KOBOeReader"

[ui]
theme = "default"           # reserved for future themes
```

## Out of scope for v1

- Cover art display (sixel / kitty graphics protocol).
- Reading progress sync from `KoboReader.sqlite`.
- Multi-library support.
- Drag-and-drop import (require typed paths instead).
- Bulk metadata fetching from online sources.

---

# Part 3 — Phasing

Build in stages so something works end-to-end at each step.

## v0.1 — skeleton

**ebookfs:**
- Read-only 9P server.
- In-memory index built by walking the library on startup (no SQLite yet).
- Exposes `by-author/` and `by-id/` only. Per-book directory exposes `book.epub` only.
- No inbox, no `ctl`, no writes.

**ebooktui:**
- Mount works, can browse `by-author/` in a single-panel list.
- No detail pane, no editing.

**Goal:** prove the 9P transport works end-to-end and the TUI can render a remote library.

## v0.2 — index and search

**ebookfs:**
- SQLite index, derived from filesystem on startup.
- Add `by-tag/`, `by-series/`, `by-status/`, `recent/`, `stats`.
- `search/` namespace with title/author/fts queries.
- Per-book directory adds `metadata`, `cover.jpg`, `id` (all read-only).
- `--reindex` flag.

**ebooktui:**
- Three-panel layout.
- Detail pane.
- `/` search.
- Scope selector switches between by-author/by-tag/by-series.

## v0.3 — writes

**ebookfs:**
- Per-book directory makes `metadata`, `tags`, `status`, `rating` writable.
- `meta.toml` round-trips.
- Buffered write + clunk-flush semantics.
- Validation on write.

**ebooktui:**
- `e` opens metadata in `$EDITOR`.
- `t` (tag modal), `s` (status cycle), `r` (rating modal).
- Multi-select with Space.

## v0.4 — inbox and ctl

**ebookfs:**
- Synthetic `inbox/` 9P node with full ingestion pipeline (Tcreate/Twrite/Tclunk → parse/validate/move/index).
- `ctl` file with batch commands.
- Local loopback mount of the 9P export on the Pi (systemd mount unit), enabling external tools to write to `inbox/` via filesystem ops.

**ebooktui:**
- Inbox view (`i`) — drag-and-drop or path-paste to copy a file into `/mnt/library/inbox/`.
- Command palette (`:`) feeds ctl directly.
- Bulk operations on selection use ctl under the hood.

## v0.5 — Kobo

**ebooktui:**
- USB detection.
- `k` sideload, with progress modal.
- Eject prompt.

## v1.0

- Polish.
- Help overlay.
- Error messages everywhere.
- Logging.
- Documentation.
- A small fixture library and end-to-end test.

## Post-1.0 (when you actually want them)

- Reading progress sync from KoboReader.sqlite.
- Cover art display via sixel/kitty.
- Online metadata enrichment (OpenLibrary).
- HTTP browser interface (only if you ever want it).
- Multi-library.

---

# Part 4 — Decisions log

Record of choices made during design, so the reasoning isn't lost when revisiting.

| # | Decision | Reasoning |
|---|---|---|
| 1 | No Calibre application, no Calibre-Web | Dislike Calibre's bloat; Calibre-Web depends on Calibre's runtime and database. We do however use one Calibre CLI tool (`ebook-meta`) as a narrow runtime dependency — see decision 16. |
| 2 | Filesystem as source of truth, SQLite as derived index | Pi 3 RAM and IO budget. In-memory index too slow on cold start. Plus filesystem-as-truth survives `ebookfs` retirement. |
| 3 | Calibre-compatible directory layout | Free interop hatch with zero ongoing cost. Stable de-facto standard. |
| 4 | 9P as the only protocol | Aesthetics and architectural clarity. Keeps client/server contract minimal. Mountable from any 9P client. |
| 5 | Go for both binaries | Single language, good 9P libs, clean cross-compile to ARM, performance acceptable on Pi 3. |
| 6 | tview for the TUI | Widget-rich, simpler than Bubble Tea for a CRUD-shaped app. k9s exists as proof of scale. |
| 7 | USB-only Kobo sync, no Drive | Avoids Google ecosystem dependency. Keeps every layer owned. |
| 8 | No Kobo HTTP browser interface | "I'm away and want a book" is a hypothetical problem. Speculative design rejected. |
| 9 | Hand-write the epub *parser*; delegate writes | Reads are simple and well-defined. Writes require handling ZIP64, malformed mimetype quirks, ADEPT, EPUB 2/3 schema differences, vendor extensions — too much surface area to hand-roll responsibly. |
| 10 | `modernc.org/sqlite` over `mattn/go-sqlite3` | Pure Go, no cgo, clean ARM cross-compile. |
| 11 | `hugelgupf/p9` for 9P | Modern, 9P2000.L, active. |
| 12 | Per-book directory with synthetic files | Plan 9 idiom, makes editing first-class through filesystem ops, `$EDITOR` works for free. |
| 13 | `ctl` file for batch operations | Avoids 100-round-trip patterns over 9P; Plan 9 idiomatic. |
| 14 | TUI talks to `/mnt/library` only, never directly to 9P | Lets TUI be tested against a fake directory; keeps surface minimal. |
| 15 | Epub's internal OPF is canonical for bibliographic metadata; `meta.toml` carries only sidecar extras | One source of truth for bibliographic data. The Kobo and every other reader read OPF from inside the epub, so editing metadata must update the file itself. Avoids drift between sidecar and embedded OPF entirely. |
| 16 | Shell out to `ebook-meta` for epub writes | The Go ecosystem has no maintained library for editing existing epubs. `ebook-meta` is Calibre's single-purpose CLI for exactly this, with 15+ years of edge-case handling. Subprocess invocation; no Calibre daemon runs. Narrow, contained dependency. |
| 17 | Id collisions during reindex are fatal, never silently renumber | Duplicate ids almost always indicate user error (restored backup, manual copy). Silent renumbering would break stable references in the Kobo filename mapping. Fail loud. |
| 18 | `inbox/` is a synthetic 9P directory, not a real filesystem path | `Tclunk` provides an explicit transaction boundary — no fsnotify, no partial-write races, no `.failed` files lingering on disk. Errors propagate synchronously to the writing client. Drops the fsnotify dependency entirely. |
| 19 | The Pi loopback-mounts its own 9P export | Re-enables "drop a file in a directory" workflows for external tools (LazyLibrarian, scripts) without compromising the 9P-only ingestion contract. Local and remote tools share one code path. |
