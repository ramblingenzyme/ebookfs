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

### `Edit` sidecar write is no longer transactional (already mitigated)

The `setDirty` design in `withTx` means `meta.toml` is now written *inside*
the same `withTx` callback as the index row — the store write happens after
`setDirty` and before the index write + `dirty=0`. If `WriteMeta` fails, the
index is left dirty and reindex recovers.

However, if `WriteMeta` succeeds and the subsequent index write fails, the
meta on disk and the `setDirty + dirty=0` sequence means the flag stays set
and reindex runs. The store (meta.toml) is the authority, so reindex reads
it back correctly.

This is no longer a meaningful gap — reindex always recovers the correct
state in any crash or partial-failure scenario.

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
|---|---|---|---|
| No `opf` file | ~30 min | Low (debugging aid) |
| Edit write order (index first) | ~1 hr | Medium (skew window) |
