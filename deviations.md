# Deviations from ebookfs-plan.md

This document catalogs the gaps between the current codebase and the plan
(`ebookfs-plan.md`). Only oversights and deferred features are listed here —
deliberate architectural choices that supersede the original plan have been
incorporated into `ebookfs-plan.md` itself.

Sections:
- [Likely oversights](#likely-oversights) — things the plan calls for that
  the code should eventually adopt.
- [Deferred features](#deferred-features) — things the plan describes but the
  phased build hasn't reached yet (not oversights, just not done).

---

## Likely oversights

### No stale inbox temp file cleanup on startup

**Plan** (§Cold start step 3):
> Ensure `inbox_temp` directory exists; clean any stale temp files left by
> previous crashed writes.

**Actual:** `main.go:29` creates the directory but never scans for or removes
stale `.tmp` files. A crash during `inboxFile.Write` leaves garbage in the
temp dir.

**Fix:** On startup, list files in `inbox_temp` and delete any that look like
temp files (e.g. matching `<prefix>*.epub` where `<prefix>` is the pattern
`os.CreateTemp` uses).

### Id collisions during reindex silently overwrite

**Plan** (§Reindex step 6 and decision #17):
> **Id collisions during reindex are fatal.** If two `meta.toml` files claim
> the same id, abort reindex and surface the conflict — never silently
> renumber.

**Actual:** `putBook` (`index/books.go:286-306`) uses:
```sql
INSERT INTO books (...) VALUES (...)
ON CONFLICT(id) DO UPDATE SET ...
```
This UPSERT silently overwrites the existing row. A duplicate id is absorbed
without error.

**Fix:** Change `ON CONFLICT(id) DO UPDATE SET` to a plain `INSERT`. An id
collision would then fail with a constraint violation, which `Rebuild` would
return as a fatal error.

### No SQL indexes

**Plan:** Four indexes:
- `idx_books_status ON books(status)`
- `idx_books_pubdate ON books(pubdate)`
- `idx_books_date_added ON books(date_added)`
- `idx_authors_sort ON authors(sort_name)`

**Actual:** No indexes at all (besides the implicit PK and UNIQUE indexes).
Every `Filter`-based query — listing by author, status, tag, series — does a
full table scan on `books` + related tables.

**Fix:** Add the four planned indexes, plus an index on
`book_authors(author_id)` (the join column used by the `by-author` filter
subquery).

### `sort_title NOT NULL` instead of nullable

**Plan:** `sort_title TEXT` (nullable — no sort title means NULL).

**Actual:** `sort_title TEXT NOT NULL`. When no sort title is available, an
empty string is stored.

**Why this is an oversight:** `NULL` is a clearer "not set" sentinel. Empty
string can be confused with "the sort title is literally an empty string."
It also prevents indexes from excluding null rows (not relevant until
indexes exist, but still a schema anti-pattern).

**Fix:** Change to `sort_title TEXT` (nullable) and insert `NULL` when no
sort title is available.

### No minimum-field validation in `Ingest`

**Plan** (§Inbox ingestion via 9P, step 5):
> Validate: must have at minimum a title and one creator. On validation
> failure: delete temp, return error.

**Actual:** `library.Ingest` calls `epub.Parse` and `bibFromEpub` but never
verifies that the result has a non-empty title and at least one author. A
corrupt or edge-case epub with no title would be ingested successfully.

**Fix:** After `bibFromEpub`, check `bib.Title != ""` and
`len(bib.Authors) > 0` before proceeding. Return a `ValidationError` on
failure.

### `Edit` sidecar write is not transactional

**Plan** (§Sidecar write via 9P):
> Begin SQLite transaction. Update `books.status`. Rewrite `meta.toml` on
> disk. Commit.

The plan implies the meta.toml write and the index update are coordinated.

**Actual:** `library.Edit` calls `store.WriteMeta` first, then `index.Put`
sequentially. If `index.Put` fails, `meta.toml` has already been updated —
they're out of sync until the next reindex. The comment in `library.go:183`
notes this gap: "If the index delete then fails… reindex walks the
filesystem and drops the stale row."

**Fix:** Swap the order — update the index first, then write `meta.toml`. If
the meta write fails, the index has been updated but meta.toml is stale —
still a skew, but the authoritative store can recover it. A better fix would
be a compensating write to restore the meta.toml on index failure, or a
cross-system transaction (outside the current scope).

### `"wtf"` error message in `inbox.Write`

**File:** `inbox.go:88`
```go
return 0, errors.New("wtf")
```

**Fix:** Replace with a descriptive error message, e.g.
`"file not opened with this fid"`.

### No `opf` file in per-book directory

**Plan** (§Per-book directory file semantics):
> | `opf` | epub | `0444` | raw OPF XML extracted from the epub | (denied) |

**Actual:** Not implemented.

**Fix:** Add a read-only static file serving `epub.ExtractOPF` (or equivalent)
bytes. Low effort, useful for debugging.

---

## Deferred features

These are called out in the plan or known to be needed but the phased build
hasn't reached them yet. They are not oversights. Listed here to distinguish
"not done" from "should be done."

### Overwrite vs append for array fields (authors, tags)

The 9P `Twrite` message carries an offset. The current implementation
ignores it — writes are buffered and committed wholesale on close, always
replacing the entire value.

A natural 9P-idiomatic enhancement would key on the write offset:
- `write(fid, offset=0, data)` → **overwrite** the value (like `>`)
- `write(fid, offset=<current-length>, data)` → **append** to the value
  (like `>>`)

This would let a client add a single tag or author without round-tripping
the full list. The `fieldFile` layer would need to distinguish between the
two modes: on overwrite the buffered value replaces the field; on append the
data is added to the existing list (parsed as newline-separated entries).
Atomicity with the index update follows the same pattern as full-field edits
— written on close, validated through `model.Edits.Validate`.

### 9P namespace views

| Planned | Status | Phase |
|---|---|---|
| `by-tag/` | Not implemented | v0.2 |
| `by-status/` (unread/reading/read/abandoned) | Not implemented | v0.2 |
| `recent/` (last 50 by date_added) | Not implemented | v0.2 |
| `search/{title:,author:,tag:,series:,status:,fts:,id:}` | Not implemented | v0.2 |

### FTS5 full-text search

`Search()` in `internal/backend/index/search.go` is a `panic("not yet
implemented")` stub. `books_fts` virtual table not in schema. Planned for
v0.2.

### `ctl` file

Planned for v0.4 (inbox + batch operations). Not implemented.

### `stats` file

Planned for v0.2. `Stats()` is a `panic("not yet implemented")` stub.

### `ebooktui` binary

The entire second half of the plan (ebooktui TUI, Kobo detection, sideload,
USB sync) is out of scope for this repository.

---

## Summary of oversight fix effort

| Fix | Effort | Impact |
|---|---|---|
| "wtf" error message | minutes | Low (cosmetic) |
| No `opf` file | ~30 min | Low (debugging aid) |
| Stale temp cleanup | ~30 min | Medium (stale files on disk) |
| Ingest minimum-field validation | ~30 min | Medium (corrupt epubs) |
| `sort_title` nullable | ~1 hr | Low (schema hygiene) |
| Reindex id collision → INSERT | ~30 min | Medium (data loss on duplicate) |
| Edit write order (index first) | ~1 hr | Medium (skew window) |
| SQL indexes | ~2 hr | High (performance at scale) |
