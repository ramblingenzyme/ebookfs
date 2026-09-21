# TODO

Unordered. Nothing here blocks anything else unless it says so.

## Library surface

- **Exporter registry.** Named exporter factories register themselves, replacing
  the hardcoded epub/kepub switch. An exporter controls the format of the
  rendered file, never its on-disk name, so the directory tree stays the same
  whichever one is configured.
- **Ingest pipeline hooks.** Convert or rewrite a file before it is parsed, and
  amend the bibliographic record before it is committed. A hook gets a
  per-ingest temp directory that is cleaned up either way.
- **Book event subscribers.** Notify subscribers after a book is ingested,
  edited or deleted. Events fire synchronously under the operation's lock, so a
  subscriber must not block or call back in. The 9P registry becomes the first
  subscriber instead of holding a library reference.
- **Book sidecar files.** Read and write auxiliary files inside a book's
  directory, carried along by renames and removed on delete. The kepub cache
  moves out of its own directory and becomes one of these, dropping the
  cache-dir config and its invalidation logic.
- **Custom metadata namespaces.** Somewhere for per-book data the built-in
  sidecar fields don't cover: purchase date, read count, notes, shelf location.
  Each handler declares a namespace and serializes into its own sidecar file.
  The built-in fields stay fixed.
- **Extended query methods.** Text search, pagination, sort field and direction,
  a count that skips hydration, and author/tag listings for browse navigation.
  These serve query-driven frontends; the 9P frontend needs none of them.

## Behaviour

- **Format-agnostic cover replacement.** A new cover image must currently match
  the existing entry's format, so a PNG over a JPEG cover is rejected. Update
  the manifest entry's filename and media type when the format changes. Still
  zip surgery, still no transcoding.
- **Persist the `ctl` command log.** It is an in-memory ring buffer today, lost
  on restart. Append each entry to a plain file outside the namespace and reload
  it at startup. Nothing derives from it, so drift detection is unaffected. No
  cap: the rate is one admin typing.
- **Per-book edit history log.** A sidecar recording edits as they happen,
  written and read straight through and never parsed back into the index. The
  store looks up `meta.toml` and the epub by name rather than enumerating a book
  directory, so an extra file beside them is invisible to it. Worth building on
  the sidecar interface above rather than twice.

## Packaging

- **Module extraction.** The library must become its own module for third-party
  frontends to depend on it. Likely a nested module with a `replace` directive
  during development. Open questions:
  - Separate repository, or a nested `go.mod` in a workspace?
  - How does its release cadence relate to the binary's?
- **Where the epub package lands.** The library imports it, so a library module
  would depend on the parent. Either epub takes a module of its own, which suits
  a package with no ebookfs dependencies, or it moves under the library at the
  split. The test corpus serves both suites from the main module and has to go
  somewhere too.

## Frontends

Both sit outside the library and use only its public surface. Neither replaces
the 9P server.

- **OPDS catalog.** Serve the library as an OPDS 1.2 / 2.0 feed, the protocol
  ebook reader apps speak. The grouping views map to acquisition feeds, search
  maps to OpenSearch, and download goes through the configured exporter.
- **HTTP API.** A JSON API for web UIs and CLI tools: paginated list endpoints
  with search and filter, standard CRUD, and endpoints for sidecar files.

## Not planned

- A plugin loader. Registration is compile-time linking.
- Config reload at runtime. Exporters and hooks are set when the library opens.
- Multi-process or distributed operation. Subscribers are in-process.
- A public store type. Capabilities go through the library.

## Done

- 9P server, SQLite index, and the views in [docs/namespace.md](./docs/namespace.md).
- Writable metadata: sidecar fields update `meta.toml`, bibliographic fields
  rewrite the epub's package document atomically.
- Inbox ingestion over 9P, with in-flight writes buffered and the clunk as the
  transaction boundary.
- KEPUB renditions in `reader/`, with an on-disk cache and a warmer.
- Clone-style `search/` handles and a root `ctl` file, including the bulk
  `rename-tag`, `rename-author` and `rename-series` commands.
- Startup reindex, narrowed to run only when the store and index disagree.
- Multi-author filenames joined with `" & "`.
- Interface segregation, done the other way round from the original plan: the
  library is a concrete struct and each frontend package declares the interface
  it consumes, so adding a method stays additive.

See [CHANGELOG.md](./CHANGELOG.md) for when each landed.
