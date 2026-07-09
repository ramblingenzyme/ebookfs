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
```

Author directories use `Surname, Forename` form (the OPF `file-as` / `sort_name`
convention). When a book has no authors, the directory is `Unknown/`. Book
directories are `Title (id)`. The `(id)` is the integer primary key from the
SQLite index — it gives every book a stable, short identifier that survives
renames.

Epub filenames are `Title - FirstAuthor.epub` (single-author form). Both title
and author are sanitised for FAT filesystems (see `internal/backend/naming/`).
Books with no authors produce `Title.epub`.

The `inbox/` node exists only in the 9P namespace — see [10P namespace > Inbox](#inbox-1).

## `meta.toml` sidecar schema

Stored at `library/<author>/<book-dir>/meta.toml`. Pure TOML, hand-editable, version-controlled.

```toml
id = 1042
date_added = "2026-05-07T14:23:00Z"
date_modified = "2026-05-07T14:23:00Z"
status = "unread"            # unread | reading | read | abandoned
rating = 0                   # 0-5, 0 = unrated
custom_tags = ["sci-fi", "feminist", "classic"]
```

The `[reading]` and `[notes]` sections from the original design
(`last_position`, `last_read`, `text`) are reserved for a future Kobo sync
feature (post-1.0) and are not yet written or read by the server.

**Bibliographic metadata (title, authors, series, identifiers, language, pubdate, description) lives inside the epub's own OPF — `OEBPS/content.opf` within the zip.** This is the canonical source. The Kobo, every reader app, and every other tool reads metadata from there. There is no sidecar OPF — it would just duplicate what the epub already contains, and the two would inevitably drift.

`meta.toml` carries only the things that don't belong in OPF: our internal id (used for the `(id)` directory suffix), reading status, rating, ebookfs-specific custom tags, and reading position. Editing the title or author of a book means rewriting the epub's internal OPF, not editing a sidecar.

## SQLite index

Lives at `library/.index.db` — never accessed over 9P. Pure cache. Rebuilt from
the filesystem on every server start (see [Cold start](#cold-start)).

Use `modernc.org/sqlite` (pure Go) so the Pi binary cross-compiles without cgo.

```sql
-- Planned schema

CREATE TABLE books (
    id            INTEGER PRIMARY KEY,
    title         TEXT    NOT NULL,
    sort_title    TEXT,                      -- nullable; NULL = no sort title
    pubdate       TEXT,
    description   TEXT    NOT NULL DEFAULT '',
    language      TEXT    NOT NULL DEFAULT '',
    library_path  TEXT    NOT NULL,
    epub_filename TEXT    NOT NULL,
    cover_path    TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'unread',
    rating        REAL    NOT NULL DEFAULT 0,
    date_added    TEXT    NOT NULL,
    date_modified TEXT    NOT NULL,
    series_id     INTEGER REFERENCES series(id) ON DELETE SET NULL,
    series_index  REAL
);

CREATE TABLE authors (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    name      TEXT NOT NULL UNIQUE,          -- display form: "Ursula K. Le Guin"
    sort_name TEXT NOT NULL                  -- file-as form: "Le Guin, Ursula K"
);

CREATE TABLE book_authors (
    book_id   INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES authors(id) ON DELETE CASCADE,
    role      TEXT NOT NULL DEFAULT 'aut',   -- aut, edt, ill, trl
    position  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (book_id, author_id, role)
);

CREATE TABLE series (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    name      TEXT NOT NULL UNIQUE,
    sort_name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE book_tags (
    book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, tag_id)
);

CREATE TABLE identifiers (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    scheme  TEXT    NOT NULL,                -- isbn, uuid, openlibrary, etc.
    value   TEXT    NOT NULL,
    UNIQUE (book_id, scheme)
);

CREATE TABLE book_id_seq (id PRIMARY KEY AUTOINCREMENT);

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

Schema versioning lives in a `pragma user_version`. Migrations are append-only —
never edit a past migration. Currently at v3 (without indexes, FTS5, role,
sort_name, or ON DELETE SET NULL); the planned schema above represents the
target state.

## 9P namespace

The synthetic filesystem `ebookfs` exposes. This is the contract.

```
/
├── books/                       ← flat listing of all books by title
│   └── The Left Hand of Darkness (1042)/
│       ├── book.epub            ← real file, read-only stream
│       ├── title                ← read/write
│       ├── authors              ← newline-separated, read/write
│       ├── series               ← single-line, read/write
│       ├── series_index         ← single number, read/write
│       ├── language             ← single-line, read/write
│       ├── description          ← read/write
│       ├── pubdate              ← read-only
│       ├── cover.jpg            ← read/write (only when epub declares a cover)
│       ├── tags                 ← newline-separated, read/write
│       ├── status               ← single-line, read/write
│       ├── rating               ← single integer, read/write
│       └── id                   ← read-only "1042"
├── by-author/
│   └── Le Guin, Ursula K/
│       └── (book directories, like books/)
├── by-series/
│   └── Foundation/
│       ├── 1 - The Left Hand of Darkness
│       └── 2 - Foundation and Empire
├── by-tag/
│   └── sci-fi/
│       └── (book directories, like books/)
├── by-status/
│   ├── unread/
│   ├── reading/
│   ├── read/
│   └── abandoned/
├── by-id/
│   └── 1042. The Left Hand of Darkness/
│       └── (book directory)
├── recent/                      ← last 50 added, by date_added desc
├── search/
│   ├── title:foundation/        ← walk to a synthetic dir, get matches
│   ├── author:asimov/
│   └── fts:robot+laws/
├── inbox/                       ← write here to ingest (purely synthetic)
├── reader/                      ← export view for rsync-to-Kobo
│   └── Le Guin, Ursula K/       ← all authors joined with " & "
│       └── The Left Hand of Darkness.epub  ← or .kepub.epub when Convert
├── ctl                          ← command file for batch ops
└── stats                        ← read-only library statistics
```

### Per-book directory file semantics

Each bib/sidecar field is a separate 9P file, backed by either the epub's
internal OPF (bib fields) or `meta.toml` (sidecar fields). The combined
`metadata` TOML file from the original design was replaced by individual
field files — more Plan 9-idiomatic and avoids TOML parsing on every write.

- **OPF-backed** (rewrites the epub): `title`, `authors`, `series`,
  `series_index`, `language`, `description`, `cover.jpg`. Writes to these
  rewrite the epub via the hand-written OPF XML editor
  (`internal/backend/epub/write.go`).
- **Sidecar-backed** (lives in `meta.toml`): `tags`, `status`, `rating`.
  Writes update `meta.toml` and the SQLite index.

| File | Backing | Mode | Read returns | Write effect |
|---|---|---|---|---|
| `book.epub` | epub | `0444` | streams the actual epub bytes | (denied) |
| `title` | epub | `0644` | single line, the book's title | rewrites OPF `<dc:title>` |
| `authors` | epub | `0644` | newline-separated author names | rewrites OPF `<dc:creator>` elements |
| `series` | epub | `0644` | series name | rewrites OPF collection / calibre:series |
| `series_index` | epub | `0644` | float, e.g. `"1.5"` | rewrites OPF group-position / calibre:series_index |
| `language` | epub | `0644` | BCP 47 code | rewrites OPF `<dc:language>` |
| `description` | epub | `0644` | book synopsis | rewrites OPF `<dc:description>` |
| `pubdate` | epub | `0444` | ISO 8601 publication date | (denied) |
| `cover.jpg` | epub | `0644` | extracted cover image bytes | rewrites the cover zip entry in-place |
| `tags` | sidecar | `0644` | newline-separated tag list | updates `meta.toml` and index |
| `status` | sidecar | `0644` | single line, e.g. `reading\n` | validates against enum, updates `meta.toml` and index |
| `rating` | sidecar | `0644` | single integer 0-5 | validates, updates `meta.toml` and index |
| `id` | sidecar | `0444` | integer | (denied) |

**Write transactionality:** writes are buffered server-side and flushed on
`Tclunk`. Partial writes don't corrupt state. If a write fails validation
(e.g. invalid status, rewrite error), `Tclunk` returns an error and the
buffered content is discarded. For epub-backed writes, the OPF is rewritten
surgically and the zip is replaced atomically via temp-file + rename — a
failed rewrite leaves the original intact.

### Search directories

`search/` is a virtual root. Walking to a name like `author:asimov` triggers a
query parse. Reading the directory returns matching book entries (as symlinks
back into `by-id/` or as the per-book directories themselves — implementation
choice).

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

Plan 9-style control file. Write a command line, server parses and executes.
Reading returns a summary of the last command's result.

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

`inbox/` is **purely synthetic** — there is no real `inbox/` directory on
disk. It exists only as a node in the 9P namespace.

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

## Module layout (actual)

```
ebookfs/
├── main.go                         # entry point: config, store, index, library, server
├── internal/
│   ├── backend/
│   │   ├── epub/                   # hand-written epub I/O
│   │   │   ├── types.go            # XML types for container + OPF
│   │   │   ├── parse.go            # zip + container.xml + OPF parser
│   │   │   ├── translate.go        # OPF XML → Book struct
│   │   │   ├── extract.go          # cover image extraction
│   │   │   ├── write.go            # surgical OPF XML editor (beevik/etree)
│   │   │   └── zip.go              # shared zip utilities, atomic rewrite
│   │   ├── store/                  # filesystem operations
│   │   │   ├── library.go          # Store type: AbsPath, Exists, OpenEpub, Move, Delete
│   │   │   ├── ingest.go           # inbox → canonical path
│   │   │   ├── path.go             # canonical path construction, sanitisation
│   │   │   ├── meta.go             # meta.toml read/write (atomic)
│   │   │   └── walk.go             # library tree enumeration
│   │   ├── index/                  # SQLite-backed cache
│   │   │   ├── schema.sql          # embedded (go:embed)
│   │   │   ├── index.go            # Index type: Open, Close, NextID, withTx
│   │   │   ├── books.go            # book CRUD: Query, Get, Put, Delete, Filter
│   │   │   ├── search.go           # (stub — not yet implemented)
│   │   │   └── reindex.go          # Rebuild: full index rebuild from filesystem
│   │   ├── library/                # orchestrator facade
│   │   │   └── library.go          # Library interface: Ingest, ListAll, Reindex, Edit, Delete, …
│   │   ├── naming/                 # filename sanitisation
│   │   │   └── naming.go           # Sanitize, ForFAT
│   │   └── kepub/                  # Kobo-format conversion (kepubify)
│   │       ├── convert.go          # kepubify wrapper
│   │       └── cache.go            # on-disk cache with mtime invalidation
│   ├── config/                     # TOML config parsing
│   │   └── config.go
│   ├── frontend/
│   │   └── fs/                     # 9P synthetic filesystem (go9p)
│   │       ├── fs.go               # newFS helper
│   │       ├── server.go           # StartServer, setupServer
│   │       ├── registry.go         # bookRegistry: id → bookDir, view management
│   │       ├── bookdir.go          # per-book directory + field map
│   │       ├── fieldfile.go        # generic read/write 9P file for string values
│   │       ├── bookfiles.go        # epubFile, coverFile
│   │       ├── booklist.go         # all-books flat listing
│   │       ├── view_author.go      # by-author grouped view
│   │       ├── view_series.go      # by-series grouped view
│   │       ├── view_id.go          # by-id flat listing
│   │       ├── viewbase.go         # shared groupingDir base, namedBookDir
│   │       ├── reader.go           # reader/ export view, warmer
│   │       ├── inbox.go            # synthetic inbox directory + file
│   │       └── view_tag.go         # by-tag grouped view
│   │       ├── view_status.go      # by-status grouped view
│   │       └── basefile.go         # snapshotFile, readAtFile
│   └── shared/
│       └── model/                  # shared types
│           └── model.go            # Book, Bib, Meta, Edits, Location, Author, …
├── go.mod
└── go.sum
```

## Library choices

| Concern | Pick | Why |
|---|---|---|
| 9P implementation | `github.com/knusbaum/go9p` (forked) | Simpler API for synthetic filesystem patterns; actively maintained fork |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no cgo, clean ARM cross-compile |
| Config | `github.com/BurntSushi/toml` | TOML stdlib equivalent |
| Logging | `log` (stdlib) | Small project; structured logging deferred |
| Epub parser | Hand-written (`internal/backend/epub/parse.go`) | Zip + OPF parsing is small and well-defined |
| Epub writer | Hand-written (`internal/backend/epub/write.go`, uses `beevik/etree`) | Eliminates Calibre runtime dependency; surgical OPF XML editing + atomic zip rewrite |
| KEPUB conversion | `github.com/pgaskin/kepubify/v4` | Native Go conversion library for Kobo-format epubs |
| EPUB XML editing | `github.com/beevik/etree` | Round-trips XML without mangling namespace declarations |
| Testing | stdlib | No need for a framework |

The epub *parser* and *writer* are both hand-written. The parser is ~160 lines
(zip open, container.xml, OPF parsing). The writer is ~400 lines of surgical
OPF XML editing using `beevik/etree`, paired with an atomic zip rewrite that
preserves the `mimetype` entry's STORED compression. This eliminates any
Calibre runtime dependency — `ebookfs` is a static binary with no external
tools required.

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

[reader]
statuses = ["unread", "reading"]      # which statuses appear in reader/ view
convert = false                       # convert to KEPUB; requires cache_dir
cache_dir = "/mnt/storage/kepub-cache" # must be OUTSIDE library root

[server]
listen = "0.0.0.0:5640"              # TCP listen address
auth = "none"                         # "none" | "shared-secret"
shared_secret_file = ""               # path if auth = shared-secret

[log]
level = "info"
format = "text"                       # "text" | "json"
```

## Deployment

### Prerequisites

`ebookfs` has no runtime dependencies beyond the standard Linux kernel (for 9P
mounting on the client side). The server binary is statically compiled with
`CGO_ENABLED=0` and runs on any Linux system regardless of installed packages.

Optional: the KEPUB conversion path uses `github.com/pgaskin/kepubify/v4` at
build time (linked in). No external tools are needed at runtime.

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
2. Ensure `library_root` and `inbox_temp` directories exist.
3. Verify `inbox_temp` is on the same filesystem as `library_root` (via `rename(2)` probe — required for atomic ingest).
4. Open SQLite index. Apply schema if fresh (version == 0); if version mismatch, tables are dropped and recreated during reindex.
5. **Always reindex from the filesystem.** The store is authoritative; rebuilding on every start guarantees the index never drifts from the filesystem. On the target hardware (Pi 3, ~500 books) a full reindex completes in seconds.
6. Optionally initialise the KEPUB cache directory if conversion is enabled.
7. Bind 9P listener, serve.

### Inbox ingestion via 9P

1. Client issues `Tcreate inbox/<filename>`. Server allocates a temp file at
   `<inbox_temp>/<random>.epub` and associates it with the 9P fid.
2. Client streams bytes via `Twrite`. Server appends via `WriteAt` to the temp
   file (no memory pressure for large epubs).
3. Client issues `Tclunk`. Server begins ingestion.
4. **Parse epub:** open zip, find container.xml, parse OPF. On parse failure:
   delete temp, return descriptive error from `Tclunk`.
5. **Validate:** must have at minimum a title and one creator. On validation
   failure: delete temp, return error.
6. **Allocate id** from `book_id_seq` (SQLite `INSERT ... RETURNING id`).
7. **Construct canonical path** via `store.Layout(authors, title, id)`. Check
   `store.Exists` to reject duplicates.
8. **Rename and sidecar:** `mkdir` the book directory, `rename(2)` the temp
   into place, and write `meta.toml` (id + defaults).
9. **Index:** call `index.Put` to insert or update the SQLite rows. On
   failure, the store directory is deleted (compensation) and the error is
   returned.
10. Return success from `Tclunk`. The book is registered in all 9P views.

If anything between step 4 and step 9 fails: delete the temp file (if still in
inbox_temp), delete partially-created book directory, return descriptive error.
The client sees the error directly. No `.failed` files litter the disk.

### Sidecar write via 9P (status, tags, rating)

1. Client `Topen` on a sidecar field file (e.g. `/books/.../status`),
   `Twrite "reading\n"`, `Tclunk`.
2. On `Tclunk`, server validates content (e.g. status against enum, rating 0-5).
3. Server calls `library.Edit`, which updates the SQLite index first via
   `index.Put`, then rewrites `meta.toml` on disk. The book dir is re-homed in
   9P views if the title or authors changed (sidecar fields never trigger
   rehoming).
4. Return success. If validation or write fails, return error from `Tclunk`
   and discard buffered state.

### OPF write via 9P (title, authors, series, etc.)

1. Client `Topen` on a bib field file (e.g. `/books/.../title`), `Twrite` the
   new value, `Tclunk`.
2. On `Tclunk`, server constructs an `Edits` struct from the written value and
   calls `library.Edit` with it.
3. `Edit` validates the edits against the book's current state. On failure,
   return error and discard.
4. If bib fields changed (`HasBibEdits`), `epub.WriteBib` is called. This reads
   the existing zip, surgically edits the OPF XML in memory (using
   `beevik/etree`), then writes a new zip atomically via temp-file + rename.
   The result is verified by re-parsing before returning.
5. If the canonical path changed (title or authors edited), `store.Move`
   relocates the book directory using two-phase `rename(2)` with rollback on
   failure.
6. `meta.toml` is rewritten with the new `date_modified`.
7. The index is updated via `index.Put`.
8. The updated `*model.Book` is returned; the 9P registry re-homes the book
   dir in all views based on the new title/authors/status.
9. Return success from `Tclunk`. On any failure between steps 3 and 7, the
   error propagates to `Tclunk` and the write is discarded.

### Cover write via 9P

1. Client `Topen` on `/books/.../cover.jpg`, `Twrite` image bytes, `Tclunk`.
2. On `Tclunk`, server validates the image format: it must be a valid JPEG or
   PNG whose format matches the existing cover entry's extension (.jpg/.png).
   No transcoding is performed.
3. `epub.WriteCover` opens the epub zip, reads the existing cover entry bytes,
   and writes a new zip atomically (temp-file + rename) with the cover entry
   replaced. Encryption and DRM checks prevent replacing protected covers.
4. The cover entry is verified by re-parsing the epub. The updated book is
   registered back (cover path unchanged).

### Reindex

1. Walk `library/` via `store.Walk` — enumerates directories that contain
   `meta.toml` + an `.epub` file.
2. For each entry, read `meta.toml` and parse the epub's OPF via `epub.Parse`.
   Books whose meta or epub can't be read are logged and skipped (not fatal).
3. Call `index.Rebuild(books, maxID)`, which:
   - Checks the schema version; drops and recreates all tables on mismatch.
   - Deletes all rows (child tables first for FK safety) and bulk-re-inserts
     all books.
   - Advances `book_id_seq` past `maxID` so future `NextID` calls don't collide.
4. The `id` field in `meta.toml` is preserved — that's the whole point of
   having it. Reindex doesn't allocate new ids unless a `meta.toml` is missing
   one (in which case the max is tracked and the sequence advanced).
5. **Id collisions during reindex are fatal.** If two `meta.toml` files claim
   the same id, abort reindex and surface the conflict — never silently
   renumber. The user fixes it manually (typically by deleting one of the two
   `meta.toml` files, since the duplicate almost always means a restored backup
   or a botched manual copy).

## Testing strategy

- **Unit tests** for epub parser, epub writer (edit-and-reparse round-trip),
  path sanitisation, Edits validation, and 9P file semantics (field reads,
  writes, multi-fid, close behaviour).
- **Integration tests** for the 9P server using go9p's in-process FS —
  `setupServer` is called directly, views populated, and 9P operations
  exercised through the library mock (`fakeLib`).
- **End-to-end test** with a fixture library of sample epubs, spinning up
  `ebookfs` against a temp directory and driving it via real 9P client calls.
  Exercises the full edit → rewrite path against real epubs.
- **No mocking of SQLite.** Index tests use real SQLite against `:memory:`.
- **Test infrastructure:** hand-written functional mocks (`fakeLib`,
  `testExporter`, `fakeEpubReader`). No mock generation framework.

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
- SQLite index, derived from filesystem on startup.
- Exposes `by-author/` and `by-id/`. Per-book directory exposes `book.epub` only.
- No inbox, no `ctl`, no writes.

**ebooktui:**
- Mount works, can browse `by-author/` in a single-panel list.
- No detail pane, no editing.

**Goal:** prove the 9P transport works end-to-end and the TUI can render a remote library.

## v0.2 — index and search

**ebookfs:**
- SQLite index with full CRUD (Query, Get, Put, Delete).
- All-books, `by-author/`, `by-series/`, `by-id/` views.
- `by-tag/`, `by-status/`, `recent/` views.
- `search/` namespace with title/author/fts queries.
- Per-book directory adds all field files.
- `stats` file.
- Rebuild index from filesystem on every startup.

**ebooktui:**
- Three-panel layout.
- Detail pane.
- `/` search.
- Scope selector switches between by-author/by-tag/by-series.

## v0.3 — writes

**ebookfs:**
- Per-book directory makes all field files writable.
- `meta.toml` round-trips (atomic temp-file writes).
- Buffered write + clunk-flush semantics.
- Validation on write.
- Hand-written epub writer (OPF XML surgery via `beevik/etree`).

**ebooktui:**
- `e` opens metadata in `$EDITOR`.
- `t` (tag modal), `s` (status cycle), `r` (rating modal).
- Multi-select with Space.

## v0.4 — inbox and ctl

**ebookfs:**
- Synthetic `inbox/` 9P node with full ingestion pipeline.
- `ctl` file with batch commands.
- Local loopback mount of the 9P export on the Pi (systemd mount unit),
  enabling external tools to write to `inbox/` via filesystem ops.

**ebooktui:**
- Inbox view (`i`).
- Command palette (`:`) feeds ctl directly.
- Bulk operations on selection use ctl under the hood.

## v0.5 — Kobo

**ebookfs:**
- `reader/` export view for rsync-to-Kobo, filtered by configured statuses.
- Optional KEPUB conversion via `kepubify/v4` with on-disk cache.
- Proactive warmer (4 goroutines) for ahead-of-time conversion.

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

- Full-text search via FTS5.
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
| 1 | No Calibre application, no Calibre-Web | Dislike Calibre's bloat; Calibre-Web depends on Calibre's runtime and database. No Calibre tools used at runtime. |
| 2 | Filesystem as source of truth, SQLite as derived index | Pi 3 RAM and IO budget. In-memory index too slow on cold start. Plus filesystem-as-truth survives `ebookfs` retirement. |
| 3 | Calibre-compatible directory layout | Free interop hatch with zero ongoing cost. Stable de-facto standard. |
| 4 | 9P as the only protocol | Aesthetics and architectural clarity. Keeps client/server contract minimal. Mountable from any 9P client. |
| 5 | Go for both binaries | Single language, good 9P libs, clean cross-compile to ARM, performance acceptable on Pi 3. |
| 6 | tview for the TUI | Widget-rich, simpler than Bubble Tea for a CRUD-shaped app. k9s exists as proof of scale. |
| 7 | USB-only Kobo sync, no Drive | Avoids Google ecosystem dependency. Keeps every layer owned. |
| 8 | No Kobo HTTP browser interface | "I'm away and want a book" is a hypothetical problem. Speculative design rejected. |
| 9 | Hand-write both the epub parser and writer | Both zip+OPF parsing and surgical OPF XML editing are well-defined problems. Using `beevik/etree` for round-trip-safe XML edits plus an atomic zip rewrite gives a zero-dependency static binary. Outcome: no Calibre or external tools required at runtime. |
| 10 | `modernc.org/sqlite` over `mattn/go-sqlite3` | Pure Go, no cgo, clean ARM cross-compile. |
| 11 | `knusbaum/go9p` for 9P (forked) | Simpler API for synthetic filesystem construction versus `hugelgupf/p9`. Active personal fork at `ramblingenzyme/go9p`. |
| 12 | Per-book directory with synthetic files | Plan 9 idiom, makes editing first-class through filesystem ops, `$EDITOR` works for free. |
| 13 | `ctl` file for batch operations | Avoids 100-round-trip patterns over 9P; Plan 9 idiomatic. |
| 14 | TUI talks to `/mnt/library` only, never directly to 9P | Lets TUI be tested against a fake directory; keeps surface minimal. |
| 15 | Epub's internal OPF is canonical for bibliographic metadata; `meta.toml` carries only sidecar extras | One source of truth for bibliographic data. The Kobo and every other reader read OPF from inside the epub, so editing metadata must update the file itself. Avoids drift between sidecar and embedded OPF entirely. |
| 16 | Hand-write the epub writer rather than shelling out to `ebook-meta` | Eliminates the Calibre runtime dependency; `ebookfs` becomes a static binary with zero external tool requirements. The writer uses `beevik/etree` for round-trip-safe OPF XML editing and an atomic zip rewrite that preserves the `mimetype` entry's STORED compression. No more complex than the subprocess+temp-file pattern `ebook-meta` would have required. |
| 17 | Id collisions during reindex are fatal, never silently renumber | Duplicate ids almost always indicate user error (restored backup, manual copy). Silent renumbering would break stable references in the Kobo filename mapping. Fail loud. |
| 18 | `inbox/` is a synthetic 9P directory, not a real filesystem path | `Tclunk` provides an explicit transaction boundary — no fsnotify, no partial-write races, no `.failed` files lingering on disk. Errors propagate synchronously to the writing client. Drops the fsnotify dependency entirely. |
| 19 | The Pi loopback-mounts its own 9P export | Re-enables "drop a file in a directory" workflows for external tools (LazyLibrarian, scripts) without compromising the 9P-only ingestion contract. Local and remote tools share one code path. |
| 20 | Always reindex from the filesystem on every startup | Safety over performance. The index is a derived cache; rebuilding on every start guarantees it never drifts from the authoritative store. On the target hardware (Pi 3, ~500 books) a reindex completes in seconds. |
| 21 | Individual field files instead of combined TOML `metadata` file | More Plan 9-idiomatic (one value per file). Avoids TOML parsing on every write. Each editable field is ~5 lines of declarative config in the `fields` map. The combined `metadata` file was in the original design but was replaced as an unnecessary abstraction layer. |
| 22 | KEPUB conversion for Kobo-format output | Post-plan feature. Native KEPUB avoids on-device conversion and fixes formatting issues on Kobo devices. Uses `kepubify/v4` with an on-disk cache (per-book mutex locks, mtime-based staleness). The `reader/` 9P export view serves either original epub or converted kepub. |
| 23 | Logging via `log` package | Small project; structured logging adds little value today. Migration to `log/slog` would be mechanical when desired. |
| 24 | Array fields (authors, tags) **overwrite** by default; append via write-offset convention deferred | `Twrite` carries an offset: `write(fid, 0, data)` overwrites (`>`), `write(fid, current-length, data)` appends (`>>`). Current `fieldFile` ignores the offset — always overwrites. Append would parse the existing value, add newline-separated entries, and commit. |
