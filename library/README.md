# library

Package `library` is the orchestrator facade between storage (store, index) and the 9P frontend. The frontend depends only on the `Library` and `Exporter` interfaces defined here — it never sees a filesystem path, a config struct, or a FAT sanitization call.

## Interfaces

- **`Library`** — book collection operations: ingest, list, edit, delete, open epub, extract cover/OPF, reindex
- **`Exporter`** — per-book export rendition (original epub or KEPUB); the swap point for reader/ view behavior

## Key Types

- **`IngestHandle`** — returned by `Library.CreateIngest()`; the frontend writes upload bytes via `WriteAt`, then calls `Ingest()` to finalize (close file, parse epub, lay down in store, clean up temp)

## File Split

| File | Contents |
|------|----------|
| `library.go` | Interfaces (`Library`, `Exporter`), `Open()`, helpers |
| `impl.go` | `libraryImpl` struct + most method implementations (Query, Search, Edit, Delete, Content, ...) |
| `ingest.go` | `IngestHandle` type + `ingestHandle` (file-backed) implementation |
| `reindex.go` | Startup drift detection and full-rebuild scan (`scanState`, index reconciliation) |
| `export.go` | `newExporter()`, `kepubCache`, `epubExporter`, `exportDirname` |

## What's Encapsulated

- Storage paths (library root, inbox temp)
- FAT filename sanitization (`naming.ForFAT`)
- Inbox temp file creation and cleanup
- Reader config (status set, convert flag, cache dir)
- Exporter lifecycle (warmer, cache) — managed by `Library.Close()` via type assertion
