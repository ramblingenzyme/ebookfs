# Deferred features

Features from `ebookfs-plan.md` that the phased build hasn't reached yet.
Deliberate architectural choices that supersede the original plan have been
incorporated into the plan itself.

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


