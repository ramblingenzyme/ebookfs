# Deferred features

Features from `ebookfs-plan.md` that the phased build hasn't reached yet.
Deliberate architectural choices that supersede the original plan have been
incorporated into the plan itself.

These are called out in the plan or known to be needed but the phased build
hasn't reached them yet. They are not oversights. Listed here to distinguish
"not done" from "should be done."

### Overwrite vs append for array fields (authors, tags) — Done

`fieldFile.Open` now honours `proto.Otrunc` (the Plan 9 `OTRUNC` flag in the
open mode). When a client opens with `OTRUNC` (shell `>`), the write buffer
starts empty — the first write replaces the whole value. Without `OTRUNC`
(`>>` or in-place edit), the buffer starts as a copy of the current value, so
writing at the end naturally appends. Validation and atomicity still flow
through `model.Edits.Validate` on close.

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


