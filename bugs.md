# Bugs

Found reading the code, 2026-10-10. "Reproduced" means a throwaway test showed
it; the rest come from reading the code. An entry marked **Fixed** has its fix
and a test in the working tree, not yet committed.

High: data loss or a library that will not open. Medium: wrong behaviour on an
ordinary path. Low: an edge case or a cosmetic problem.

## Status

Fixed: the three `pkg/epub` bugs (Save closing the Book, contributor roles,
same-name contributors), repeated list entries bricking startup, five Low code
bugs (ctl's "ok" on total failure, percentage cover sizes, subject order,
publication dates, unknown config keys), five more Low code bugs (sidecar
directories, package-document size, bare `/sync`, CHANGELOG, derived paths),
and 46 of the 48 test-code entries.

Open, highest first:

- **Code:** every remaining entry is deferred with a `ponytail:` comment that
  names the fix. High: more than 32,766 books will not start. Medium: long
  names cannot be ingested; reader/ stats wait behind kepub conversions; three
  kosync items; rejected 9P writes look like success on Linux; 9P rename and
  chmod rewrite the tree in memory. Low: the rest.
- **Tests, hard:** a real "store failure keeps the pending row" test.
- **Needs a decision:** `TestAuthorRenameDropsRefinements` against the
  checklist's "data loss"; whether to bump the index schema so existing books
  pick up every contributor role.

## pkg/library/internal/index

### High: a repeated tag, subject or contributor bricks startup

**Fixed.** `book.Validate` rejects a repeated tag, subject or contributor-in-role in an edit, as it already did authors. An epub or a hand-edited `meta.toml` never passes `Validate`, so reading either keeps the first of each repeat. That also keeps a repeated author out of the book's directory name.

Reproduced. `putRelations` writes each list into a join table keyed
`(book_id, x_id)` with a plain `INSERT`, and nothing upstream removes
duplicates from tags, subjects or contributors. `Edits` validation rejects
repeated authors only.

Writing `a\na` to a book's `tags` or `subjects` file fails with `UNIQUE
constraint failed: book_tags...`. By then `Library.Edit` has already rewritten
the epub and `meta.toml`, so the pending op stays. The next startup rebuilds,
and `Index.Rebuild` hits the same constraint inside its single transaction.
`library.Open` then fails with `reindexing library: constraint failed`, and
the server will not start until someone edits the file by hand.

An epub whose OPF repeats a `dc:subject`, a `dc:creator` or a
`dc:contributor` (same name and role) takes the same route. Ingest refuses it
with a raw SQL error, and the same file copied into the root bricks startup.

Fix: drop duplicates before the join-table inserts, keeping the first
occurrence so author and contributor positions survive. Rejecting duplicates
in `book.Validate` as well would give the 9P client a readable error.

### High: a library of more than 32,766 books will not start

**Deferred.** A `ponytail:` comment on `hydrateBooks` records the limit and the fix. Libraries that size are not a target yet.

Reproduced at the query level. `hydrateBooks` loads authors, tags and the
rest with `WHERE book_id IN (sqlc.slice(...))`, which binds one variable per
book. SQLite's limit is 32,766, so 32,767 ids fail with `too many SQL
variables`. `fs.New` loads the whole library through `Search(Query{})`, so
past that size the 9P frontend fails to build and the process exits. kosync's
`Rebuild`, the OPDS "All Books" feed and `ctl ... *` fail the same way. Fix:
hydrate in chunks, or join against the outer query instead of passing ids
back.

### Low: title search folds case differently in search/ and everywhere else

**Deferred.** A `ponytail:` comment on the index's title search records the gap and the fix.

`Query.Titles` promises a case-insensitive substring match. SQLite's `LIKE`
folds ASCII only, so `title:émile` misses "Émile Zola" in `ctl` and OPDS.
The 9P `search/` handles match in Go with `strings.ToLower`, which folds
Unicode, so the same query finds the book there. `search_parity_scenario_test.go`
already knows: it skips both non-ASCII cases as a known divergence that
needs a decision.

## pkg/library/internal/store

### Medium: a long title or author list cannot be ingested

**Deferred.** A `ponytail:` comment on the store's layout records the limit and the fix.

Reproduced. `Layout` names the book directory `title (id)` and the epub
`title - authors.epub`, and caps neither at the 255-byte `NAME_MAX`. A
154-character title with four authors fails at the rename, and a 90-character
CJK title (270 bytes) fails at `mkdir`. Ingest returns `file name too long`.
An edit that lengthens a title fails in `Store.Update` after the epub is
rewritten, leaving the index stale until the next startup's reindex.
`naming.ForFAT` has the same gap for the export filename.

## pkg/library/internal/kepub

### Medium: a stat in reader/ waits behind that book's kepub conversion

**Deferred.** A `ponytail:` comment on `Cache.Ensure` records the gap and the fix.

`Cache.Ensure` runs the whole conversion inside `WithSidecars`, which holds
the library's per-book lock. `Cache.Size` takes the same lock, and
`ReaderFile.Stat` calls it on every 9P stat. Startup queues a warm for every
book in `reader/`. An `ls -l` or rsync during that warm blocks on each book
being converted, for as long as kepubify takes on it. Edits, deletes and kosync
PUTs for the book wait too. The `os.Root` follows the directory by fd, so the
conversion could write without holding the lock and take it only for the final
rename.

### Low: a warm queued before an edit fails with a zip error

**Deferred.** A `ponytail:` comment on the kepub cache's write records the gap and the fix.

`Ensure` opens the book's current epub but passes the queued snapshot's
`EpubSize` to `zip.NewReader`. If an edit resized the file in between, the
central directory lands at the wrong offset, and the warmer logs a spurious
failure. The read path recovers, since it calls `Ensure` with a fresh book.
Taking the size from the opened file would close the gap.

## pkg/library

### Low: sidecar access recreates a vanished book directory

**Fixed.** `OpenSidecars` creates only `.sidecar/`, so a missing book directory fails with `fs.ErrNotExist`. A kosync PUT for such a book now answers 502 instead of writing into a recreated directory, which the deferred stale-entry fix would turn into "unknown document".

`Store.OpenSidecars` calls `MkdirAll` on `Author/Title (id)/.sidecar`. If the
directory was removed outside ebookfs, the next `WithSidecars`, kepub stat or
kosync request for that id recreates the empty path. The next walk skips it,
since it holds no `meta.toml`, but the empty directories stay.

## pkg/epub

### Medium: Save leaves the Book closed

**Fixed.** `rewrite` no longer defers closing the file it hands over. `TestBookStaysUsableAfterSave` covers it.

Reproduced. `Book.rewrite` opens the new file as `next`, defers
`next.Close()`, then copies `*next` into `*b`. The deferred close runs on the
`*File` that `b` now shares. After a successful `Save`, `ReadEntry` returns
`ErrClosed`, and a second `Save` fails with `file already closed`. The doc
says "After Save, the Book reads from the new file, and a later Save compares
against the values just written". `library/internal/epub.Rewrite` survives
only because it reads the metadata, which is held in memory, and never
touches the file again. Any other importer of `pkg/epub` hits this.

### Low: replacing a cover fixes a percentage width to pixels

**Fixed.** `FitCover` leaves a `width` or `height` ending in `%` alone.

`content.Doc.FitCover` rewrites any existing `width` and `height` on the
cover `<img>` or `<image>`. An HTML cover page with `<img width="100%">`
becomes `width="1200"`, which overflows the screen on a small reader. The SVG
case is fine, since the outer `<svg>` keeps its percentage. Skipping values
that end in `%` would cover it.

### Low: a subject reorder is lost

**Fixed.** A reused subject is placed in the order given, as authors and contributors are.

Found while fixing contributors. `subjectsField.set` never calls `Place` on a
subject it reuses, so writing `[New, Fiction]` over a file holding `[Fiction]`
reads back as `[Fiction, New]`. Authors and contributors reposition their
elements. The index sorts subjects by name, so nothing in ebookfs shows it.

### Low: an entry's declared size is trusted when reading it

**Fixed.** `epub.Open` refuses a package document that declares more than 16 MiB. `archive/zip` already fails a read past the declared size, so the check bounds memory before the read.

`archive.read` reads an entry with `io.ReadAll`. A small upload whose package
document inflates to gigabytes holds all of it in memory while parsing.
`archive/zip` stops at the declared size, but that size is whatever the zip
header says. On a 1 GB ARM board, one upload to `inbox/` can OOM the server.
9P has no authentication anyway, so this adds little to what a client can
already do.

## pkg/epub/internal/opf

### High: an EPUB 3 contributor's role cannot change, and role refinements pile up

**Fixed.** Roles are rewritten in place through `Refine.SetValues`, and a legacy `opf:role` is kept in step. Covered by `TestContributorRoleChangeIsWritten`, `TestContributorWriteLeavesOneRefinementPerRole` and the reworked `TestContributorsReconcilePreservesRefinements`.

Reproduced. `contributorsField.set` reuses the element matched by name, then
calls `Refine("role").Add`, which appends. It never removes the old role.
`get` reads the first role, so writing `Jane | trl` over `Jane | edt` reads
back as `edt`. Every save that touches contributors appends another role
`<meta>` to each one. Four saves left five role refinements on one
contributor.

Fix: replace the `aut`-free roles this package owns, as `authorsField.set`
does for `file-as`, rather than adding.

### Medium: two contributors with one name collapse into one

**Fixed.** EPUB 3 writes one element per person with a role refinement per credit (D.3.10) and reads one entry per role. EPUB 2 writes one element per credit. Covered by `TestContributorCreditedTwice`, `TestContributorEntriesGatherByPersonInEPUB3` and `TestSpecContributorRolesReadInOrder`.

Reproduced. `contributorsField.set` keys elements by name alone, so
`[Jane | edt, Jane | trl]` writes one `dc:contributor`. The index stores two
rows from the in-memory list, and the file holds one, until the next reindex
drops the second.

## internal/kosync

### Medium: kosync can move a book's status backwards

**Deferred.** A `ponytail:` comment on `updateBookStatus` records the gap and the fix.

`updateBookStatus` only protects `read`. A sync below `reading_threshold`
turns `reading` back into `unread`, and any sync overwrites `abandoned`. If
`reader.statuses` is `["reading"]`, the book leaves `reader/`, and the next
`rsync --delete` removes it from the device. Skimming back to the cover, or a
second device starting from page one, is enough. Needs a decision: promote
only?

### Medium: books that arrive while the mapping is non-empty never get a document ID

**Deferred.** A `ponytail:` comment on `kosync.New`, shared with the stale-entry bug below, records the gap and the fix.

`kosync.New` calls `Rebuild` only when the mapping is empty. A book that
arrives through a startup reindex, or was ingested while kosync was disabled,
never gets a sidecar or a mapping entry. KOReader's PUTs for it get a 200 and
are dropped. Rebuilding on every startup fixes it for one small sidecar read
per book.

### Medium: progress for a kepub rendition never matches

**Deferred.** A `ponytail:` comment on the hook's document-ID hash records the gap and the fix.

With `reader.convert = true` the device holds the `.kepub.epub`. Its partial
MD5 differs from the epub's, so every PUT is for an unknown document and gets
dropped. Fixing it would mean mapping the kepub's hash as well.

### Low: a stale mapping entry answers 502 forever

**Deferred.** A `ponytail:` comment on `kosync.New`, shared with the missing-ID bug above, records the gap and the fix.

If a book is removed outside ebookfs, or `OnDeleting` cannot read the sidecar,
its document IDs stay mapped. GET and PUT then fail on `ErrBookNotFound` with
a 502, rather than treating the document as unknown. `Rebuild` never runs to
clear them while the mapping is non-empty.

## internal/frontend

### Low: a bare `/sync` redirects off the prefix

**Fixed.** A frontend whose prefix is stripped gets its bare prefix redirected to the prefix with a slash. The mux is built in a method a test can call.

`Runner.Run` strips `/sync`, which leaves an empty path. kosync's inner
`ServeMux` cleans that to `/` and answers with a redirect to `/`, the site
root, rather than `/sync/`. No KOReader request hits the bare prefix.

## internal/fs

### Medium: a rejected write looks like success on a Linux mount

**Deferred.** A `ponytail:` comment on `vfile.WriteBuffer` records the gap and the fix.

Every writable file commits on clunk: field files, `cover.*`, `inbox/`
uploads and `ctl`. A failure comes back as an `Rclunk` error. Linux v9fs
clunks from `->release`, whose return value the VFS discards, so `close(2)`
returns 0. `echo 7 > rating`, a PNG written over a JPEG cover, and a `cp` of
a corrupt epub into `inbox/` all exit 0 and change nothing. The reason
appears only in the server log. DECISIONS #15 says an ingest error is
"reported straight back to whoever wrote the file", which holds for a
plan9port client but not for the kernel mount, and not for the self-mount of
#16. This comes from reading the v9fs source and was not reproduced, since
mounting needs root here. A per-book `errors` file, or failed commits
recorded in `log`, would make failures visible.

### Medium: a 9P rename or chmod rewrites the served tree in memory

**Deferred.** A `ponytail:` comment on `fs.New` records the gap and the fix.

go9p's `Wstat` writes the client's new name, mode and length into the node
through `WriteStat`. `fs.New` builds the tree with `IgnorePermissions`, so
every check passes, and no node in `internal/fs` overrides `WriteStat`. `mv
by-tag/sf by-tag/sci-fi` renames the group directory in the listing without
changing any book's tags. The registry then looks for `sf`, misses, and files
later edits under a fresh `sf`, so the two drift until restart. `mv rating
x` inside a book renames the field file the same way. `BookDir` and the epub
file are safe, since their `Stat` recomputes the name. Fix: reject
`WriteStat` on the synthetic nodes, or accept only a length change.

### Low: emptying a field file with `: >` does nothing

**Deferred.** A `ponytail:` comment on the field file's close records the gap and the fix. Linux sends the same length-0 Twstat before every `>` redirect's write, so committing on it would break ordinary edits.

`fieldFile.Close` commits only if the fid wrote bytes. A truncating open with
no write sends a `Twstat` length of 0, which go9p stores in the stat and
nothing reads. `: > description` leaves the description in place, while
`echo > description` clears it.

### Low: a book directory has one parent across every view

**Deferred.** A `ponytail:` comment on `BookDir` records the gap and the fix.

go9p's `AddChild` sets the child's parent, and the same `*BookDir` is added
to `books/`, each `by-x/` group, `recent/` and every search result, so the
last `AddChild` wins. `DeleteChild` sets it to nil. After a search, `..` from
`books/Dune` walks to `search/N/results`, and it keeps pointing there after
the handle is closed. `fs.FullPath` in go9p's error messages names the same
wrong path. Most clients resolve `..` themselves, which hides it.

### Low: colliding reader/ names drop a book, and removing one removes both

**Deferred.** A `ponytail:` comment on `readerDir.Add` records the gap and the fix.

`readerDir.Add` ignores the error from `AddChild`, which refuses a second
child of the same name. Two books by the same authors, whose titles differ
only in characters `naming.ForFAT` maps to `-` (`Dune: Part 1` and
`Dune? Part 1`), get one export name, and the second never appears in
`reader/`. `Remove` calls
`DeleteChild`, which deletes every child of that name, so removing either
takes the survivor out too.

### Low: ctl reports "ok" when every book failed

**Fixed.** A result where every matched book failed opens with `error:`, like ctl's other failures. A partial failure still opens with `ok:`.

`formatResult` always opens with `ok:`, even when `affected` is 0 and every
book is in `errs`. A script that checks the first word of the log line
treats a total failure as success.

## Docs

### Low: CHANGELOG.md does not mention kosync

**Fixed.** An `[Unreleased]` Added entry describes the server. The fixes to it landed before any release, so they need no Fixed entries.

The kosync server shipped in the last five commits, with its config section,
endpoints and sidecars, and `[Unreleased]` has no entry for it. The three
kosync fixes from this session are not listed either.

## internal/opds

### Low: some common publication dates are dropped

**Fixed.** `published` also accepts the zoneless datetime and `2006-01`.

`published` accepts RFC 3339, `2006-01-02` and `2006`. calibre and other
producers also write `2010-01-01T00:00:00` with no zone and `2010-05` with
no day. Those parse as nothing, and the feed omits the date.

## internal/config

### Low: inbox_temp and index_path do not follow library.root

**Fixed.** An unset or empty `inbox_temp` or `index_path` is derived from the configured root, as `kosync.mapping_path` already was.

`defaults` fixes both under `/var/lib/ebookfs/library`. `Load` derives
`kosync.mapping_path` from the configured root, but not these two. A config
that sets only `root = "/srv/books"` keeps the index and the upload temp dir
under `/var/lib`. If the two are on different filesystems, startup fails
with "inbox_temp must be on the same filesystem as library.root", which names
a path the user never set.

### Low: unknown config keys are dropped without a word

**Fixed.** `Load` records the keys no field reads, and main logs a warning for each once logging is configured.

`Load` discards the `toml.MetaData` that `Undecoded()` would read. A typo such
as `enabled = true` silently leaves a frontend off. The local `config.toml`
in this checkout shows it: `reader.cache_dir`, `[opds] listen` and
`[opds] base_url` are all ignored, so OPDS never starts. pre-1.0-checklist
item 9 relies on removed keys being tolerated, so a warning keeps that and
still surfaces the typo.

# Test code

Tests whose name or comment promises something the body does not check, or
whose assertion could not fail. None of these hides a known bug on its own,
but each one lets a regression through.

## pkg/library

### Medium: TestDeleteRemovesOnDisk cannot fail

**Fixed.** The path is joined with `cfg.Root`, and a stat before `Delete` proves it.

`library_ext_test.go`. It stats `book.EpubPath()`, which is relative to the
library root, from the test's working directory. That path never exists
there, so the `IsNotExist` check passes whether or not `Delete` removed
anything. `TestSearchReturnsARootRelativeEpubPath` in the same file shows the
join it needs: `filepath.Join(cfg.Root, ...)`.

### Low: TestSearchSnapshotsAreImmutable checks tags and series on a book with neither

**Fixed.** The book gets a tag and a series first, and the tags check overwrites an element.

`library_ext_test.go`. `BuildTestEpub` gives no series, so the `Series()`
block sits inside `if s != nil` and never runs. The book also has no tags,
and the tags check appends to the returned slice, which cannot change the
book's length even if the slice were shared. `TestApplyMetaClonesTags` says
as much: "Element assignment detects the sharing where append would not."

### Low: TestConcurrentDuplicateIngestRejected never checks for ErrDuplicate

**Fixed.**

`concurrency_scenario_ext_test.go`. The file header says the losing ingest
"gets ErrDuplicate". The test only counts errors, so two ingests failing for
any other reason pass as long as one succeeds.

### Low: TestKepubCacheDelegates checks "cold" on a book the library lacks

**Fixed.** `Size` runs on an ingested book.

`export_ext_test.go`. `Size` reports false because `WithSidecars` fails with
`ErrBookNotFound`, not because no conversion is cached. The message says
"Size should report cold for a book with no cached conversion".

### Low: TestWriteSidecarAtomic does not test atomicity

**Fixed** by the agreed fallback: renamed `TestWriteSidecarOverwrites` and moved into `library_ext_test.go`. Proving atomicity needs a failure between write and rename, which nothing can inject.

`sidecar_test.go`. It writes twice and reads back the second value. Nothing
checks the temp-file-and-rename, or that a failed write leaves the old
content. The file is also black-box (`package library_test`) under a
`_test.go` name, and no `sidecar.go` exists; AGENTS.md wants
`library_ext_test.go` or a `_scenario_test.go`.

### Low: TestIngestPreservesAllMetadataFields skips the fields with rules

**Fixed** for series, sort title, pubdate and author sort name. The `isbn` identifier still resolves through the XML-id fallback.

`ingest_ext_test.go`. It covers ten fields but not series, pubdate, sort
title or author sort names. Series and sort title are the two the library
translates with rules of its own (index fallback, cleared sort title).
Its `isbn` identifier also resolves only through the XML-id fallback, so it
does not show scheme detection either.

### Low: TestApplyMeta "tags cleared" wants nil but gets an empty slice

**Fixed.**

`library_test.go`. The comment says clearing is "a set edit to an empty
slice, not an absent one", and `TestApplyMetaClonesTags` "nil stays nil"
says the sidecar writer tells the two apart. This case still wants `nil`,
and `slices.Equal` treats nil and empty as equal, so it cannot see which one
`applyMeta` returned.

### Low: TestReindexMigratesToCanonicalPath "sort-name directory" has no sort name

**Fixed.** The author is now "Alice Smith".

`reindex_test.go`. The fixture files author "Alice" under `Smith, Alice/`,
but Alice has no sort name and "Smith" appears nowhere else. It proves that
any stray author directory migrates, not the sort-name layout it names.

### Low: two chmod tests fail when run as root

**Fixed.** All three skip when `os.Geteuid() == 0`.

`TestCreateIngestReadOnlyDir` (`ingest_test.go`), and
`TestDeleteWithReadOnlyDirError` and `TestWriteMetaReadOnlyDir` in
`internal/store`, skip only if `chmod` fails. Root ignores the mode, so in a
container running as root the operation succeeds and the test fails.

## pkg/library/internal/index

### Low: TestQueryRecentOrder cannot tell newest-first from oldest-first

**Fixed.**

`search_test.go`. Both books get `time.Now()` from `NewBook`, so they share a
`date_added` second, and the comment admits the test pins only the `id DESC`
tiebreak. With `date_added ASC, id DESC` it still passes.

### Low: TestFacetListingsSkipOrphans has no orphan to skip

**Fixed.** Orphans are inserted directly, and series is covered.

`reads_test.go`. `op.Delete` runs `cleanupOrphans`, so the orphaned author and
tag rows are gone before the listing runs. The `JOIN` the comment credits is
never exercised. A row inserted directly into `tags` would test it. Series is
also missing from the table.

### Low: TestStoreFailureKeepsPending and TestPreStoreRefusalKeepsNoRow test only MarkPending

**Fixed.** Dropped. The other pending-row tests in `op_test.go` cover what they check. A real store-failure test is in the hard list.

`op_test.go`. Neither drives a store write or a refusal. One calls
`MarkPending` and counts one row, the other calls `BeginOp` and counts none.

### Low: TestRebuildClearsLeakedRowsAndInsertsBooks never reaches dropAllTables

**Fixed.**

`reindex_test.go`. The comment says it exercises "the full path through
dropAllTables". `openTestIndex` stamps the schema version, so `ensureSchema`
skips the drop, and `rebuildTx` deletes rows instead.

### Low: TestRolledBackTxSurfacesErrors misses two of putRelations' writers

**Fixed.**

`errors_scenario_test.go`. The header names "putRelations and its three
writers". It now has five: `replaceSubjects` and `replaceContributors` are not
in the table.

## pkg/library/internal/store

### Low: TestMoveSameLocationNoop says Edit never calls Move with one location

**Fixed.**

`store_test.go`. `Library.Edit` calls `Store.Update` on every edit, and
`Update` calls `Move` unconditionally. Every meta-only edit takes the
equal-path return this comment calls defensive.

## pkg/library/internal/epub

### Low: TestParseDefaultsAMalformedSeriesIndex has only missing indexes

**Fixed.**

`parse_ext_test.go`. Both fixtures omit the position. A malformed one, such
as `two` or `1.`, is never parsed.

## pkg/library/internal/kepub

### Low: TestCacheEnsureWithZeroDateModified has a current date

**Fixed.** Dropped with `noopSource`. It duplicated `TestCacheEnsureCreatesFile`.

`cache_test.go`. `util.MakeMutableBook` goes through `book.NewBook`, which
stamps `DateModified` with `time.Now()`. The zero-date path is never reached.
With no cache file present, the date does not matter either. The same goes
for `TestCacheEnsureCreatesFile`'s "A future DateModified makes any cache
stale". The file also declares `noopSource`, which nothing uses.

## internal/kosync

### Low: a doc comment names a test that does not exist

**Fixed.** Dropped `TestPartialMD5_LowercaseHex`. The golden vectors already compare exact lowercase hashes.

`hash_test.go`. The comment above `TestPartialMD5_LowercaseHex` describes
`TestPartialMD5_FullFileHash`, "a hash that depends on all 12 samples". The
test checks only the format of the output.

### Low: half of TestPartialMD5_FirstOffsetIsZero cannot fail

**Fixed.** Dropped the byte-256 half.

`hash_test.go`. The byte-256 half passes whether the first sample starts at 0
or at 256, so it proves nothing about the offset. The byte-0 half carries the
test.

## internal/frontend

### Low: TestRunnerHTTPServerLifecycle never builds a Runner

**Fixed.** Replaced by `TestRunnerRoutesHTTPFrontends`, which drives the extracted mux through a stripped and an unstripped frontend.

`frontend_test.go`. It serves the fake's handler through `httptest` and
checks for a 200. No test drives a request through `Runner.Run`, so the
prefix mounting and `StripPrefix` from 1e85631 have no coverage.

### Low: TestRunnerNoHTTPFrontendsNoListener cannot see a listener

**Fixed.** Dropped.

`frontend_test.go`. Its own comment concedes "a real listener on :8080 would
have been fine". It asserts only that `Shutdown` ran once.

## internal/config

### Low: "full custom" failure messages print the wanted value

**Fixed.**

`config_test.go`. `t.Errorf("Library.Root = %q", "/custom/root")` prints the
expected value as if it were the actual one. The check itself is right.

### Low: TestHTTPBaseURLMustBeAbsolute's comment names opds.base_url

**Fixed.** `TestKOSyncValidation` covers `validateKOSync`.

`config_test.go`. The key moved to `[http]`. There is also no test for
`validateKOSync`.

## internal/book

### Low: the "halfway" rating case is not at the halfway point

**Fixed.** It uses 0.125.

`edits_test.go`. 4.565 is stored as 4.56500000000000039, so `x*100` is
456.50000000000006 and rounds up under any tie rule. A real tie such as
0.125 (12.5 exactly) would tell `math.Round` from round-half-even.

### Low: an index set while clearing the series is accepted

**Fixed.** `book.Validate` rejects a series index sent with an empty series, as it does one on a book with no series.

`edits_test.go`. "book has series, empty series edit" expects no error for
`Series: ""` plus `SeriesIndex: "1"`. The series is removed and the index is
dropped without a word, while "no series anywhere" rejects the same index.

### Low: "empty unset" language writes an empty dc:language

**Fixed.** `book.Validate` rejects an empty or blank language, since both specs require the element.

`edits_test.go`. The name says it unsets the language. `SetLanguage("")`
blanks the element instead. §5.5.2 requires a non-empty value, and EPUB 3
requires a `dc:language`.

## pkg/epub

### Medium: three "preserves refinements" tests never rewrite the file

**Fixed.** Each test now changes the list and checks that the write landed. `TestSpecMultipleRoleRefines` sets a sort name and wants the roles unchanged. `TestSpecSlashInAValueIsNotRewritten` adds an author and sets a new title with a slash, since an unchanged title is never written.

`spec_ext_test.go`. `TestSubjectsReconcilePreservesRefinements` and
`TestContributorsReconcilePreservesRefinements` write back exactly the list
they read. `Book.moved` compares against the values from `Open`, finds no
change, and `Save` returns without touching the file. The refinements survive
because nothing ran. `TestSpecMultipleRoleRefines` does the same with the
authors, and its own message calls it "a no-op edit". The second half of
`TestSpecSlashInAValueIsNotRewritten` has the same shape: the authors it
passes back equal the originals, so they are never written.

Adding or reordering one entry would exercise the reconcile. For
contributors that would also have caught the role refinements piling up
(see the `pkg/epub/internal/opf` entry above).

### Low: no test reads through a Book after Save, or saves it twice

**Fixed.** `TestBookStaysUsableAfterSave`.

`save_ext_test.go`. The comment in `TestSaveRoundTrips` says "Save leaves the
Book reading the rewritten file". It leaves the `File` closed (see the
`pkg/epub` Save entry above). Every check after `save` reads in-memory
fields, and `TestSaveIsIdempotent` reopens the file between its two saves,
so neither a `ReadEntry` nor a second `Save` on one `Book` is ever tried.

### Low: TestSetCoverRefusesNonRaster tests the decode, not the format rule

**Fixed.** Now `TestSetCoverRefusesAGIFEntry`. The SVG input joined `TestSetCoverRejectsNonImage`.

`save_ext_test.go`. `<svg/>` fails `image.DecodeConfig`, the same path
`TestSetCoverRejectsNonImage` covers. The cover entry is `cover.jpg`, so
`coverFormat`'s refusal of a `.gif` entry is never reached. An `.svg` entry
cannot reach it either, since cover detection rejects XML media types.

### Low: a third of the spec tests cite no section

**Fixed.** Each now cites its section with an "Ours:" line where needed. The duplicate language test is deleted.

`spec_ext_test.go`. The header says "Each cites the section it checks", and
AGENTS.md forbids cutting the section from a spec test. Everything from
`--- Publisher ---` down, 25 tests, cites none. DECISIONS #27 and §5.5.3.2.1
cover most of them.

### Low: TestAuthorRenameDropsRefinements pins what the checklist calls data loss

**Open, needs a decision.**

`save_ext_test.go`. Labelled "Our rule", it asserts that a rename drops the
old name's alternate-script. pre-1.0-checklist lists the same behaviour under
"Bugs" as "Data loss on an ordinary edit", and README lists it as a
limitation. One of the three has to change.

### Low: a nil series panics in the failure message

**Fixed.**

Several tests guard with `if b.Series == nil || b.Series.Index != "3"` and
then print `b.Series.Index`. If `Series` is nil, the test panics instead of
reporting. Seen in `TestSetCollectionIsNotASeries`,
`TestSaveSeriesRoundTrip`, `TestSeriesEditPreservesExistingIndex`, and in
`pkg/library/internal/epub`'s `TestWriteBibSeriesIndexOnlyKeepsName`.

### Low: two failure messages describe a different case

**Fixed.**

`save_ext_test.go`. `TestSortTitleForEPUB2UsesTheCalibreMeta` reports "a
title change left a stale calibre:title_sort" after clearing the sort title.
`TestSeriesEditPreservesExistingIndex` asks for "3.0" while checking "3".

### Low: TestRefusesToEditASignedEpub calls the parent's Fatal from a subtest

**Fixed.**

`save_ext_test.go`. The "cover edit" closure captures the outer `t` and runs
inside `t.Run`. If `SetCover` ever fails there, `FailNow` runs on the
subtest's goroutine, and `testing` panics instead of reporting the failure.

### Low: TestDCPrefix's cases are not what they say

**Fixed.** Cases renamed, and the test moved to `ns_test.go`.

`pkgdoc/metadata_test.go`. "no dc element and no declaration" runs on a fixture
whose `<metadata>` declares `xmlns:dc`. "copies the prefix an existing dc
element uses" uses a `dc:title`, and `dcPrefix` looks at titles only, so a
document with a `dcx:creator` and no title takes the fallback. The test also
sits in `metadata_test.go`, while `dcPrefix` lives in `ns.go`.

## internal/fs

### Low: three tests pin behaviour recorded above as a bug

**Open** for `TestFieldFileOtruncNoWriteDoesNotCallSet`, which changes when the deferred `: >` fix lands. `TestDispatch` changed with the ctl fix.

`TestFieldFileOtruncNoWriteDoesNotCallSet` (`book/file_fields_test.go`) asserts
that a truncating open with no write leaves the field alone, the `: >` case in
the `internal/fs` section. `TestDispatch` (`ctl/exec_test.go`) expects
`ok: no books edited` followed by an error for every book, the "ok when every
book failed" entry. Fixing either bug means changing its test.

### Low: the skipped TestBooksDirMintedNameCollidesWithLiteralTitle misdescribes the failure

**Fixed.** The skip message now describes the reproduced failure. The bug itself stays open: mint until free, check `AddChild`'s error, and unskip.

`views/booklist_test.go`. The skip says the minted entry replaces the literal
one. Reproduced: go9p's `AddChild` refuses the duplicate name, so the literal
"Foo (2)" stays and the newly added book (id 2) never appears, while `entries`
maps both ids to "Foo (2)". The second half of the message is right: removing
either id deletes book 1's entry. `bookListDir.Add` ignores the `AddChild`
error, the same gap as the `reader/` entry.

### Low: TestRecentDirOrdersNewestFirst checks membership, not order

**Fixed.** Dropped.

`views/view_recent_test.go`. A directory listing has no order to check, and
the test asserts only that both books are present. The ordering is covered by
`TestRecentDirOutOfOrderArrival`, which reads `d.all`.

### Low: TestReaderDirWithConvertEnabled has nothing converted

**Fixed.** Dropped.

`views/reader_test.go`. The mock exporter has no convert setting, and the test
expects the plain `Convert.epub` name. It is the same test as
`TestReaderDirAddIncludedStatus` under a different name.

### Low: two TestRegistry* tests never remove through the registry

**Fixed.** Renamed `TestBooksDir*`, and the helper unregisters its view.

`views/booklist_test.go`. `TestRegistryAddAndRemove` and
`TestRegistryRemoveUnknownID` remove with `removeBookFromView`, which calls
the view's `Remove` directly. `BookRegistry.remove` is not involved. Each
call to that helper also registers a fresh `NewAllBooksDir` with the registry
for the life of the test.

### Low: two TestSeriesEntryName cases named "zero-padded" show no padding

**Fixed.**

`views/view_series_test.go`. "zero-padded when maxIdx >= 10" and "zero-padded
with decimal" use index 10 at width 2, so the output carries no zero.
"single digit is zero-padded" is the case that shows one.

### Low: TestEpubFileStatWithRealFile never reads the file

**Fixed.** Merged into `TestEpubFileStatReportsRecordedSize`.

`book/file_epub_test.go`. `epubFile.Stat` takes its length from
`Book.EpubSize`, which the test sets by hand. The file it writes to disk is
never touched. `TestEpubFileStatSize` likewise says "want 0 for nonexistent
file", but existence plays no part. Both describe the stat-the-file behaviour
the index replaced.

### Low: TestCoverFileStatLengthNilLib has nothing to do with the lib

**Fixed.** Dropped.

`book/file_cover_test.go`. `coverFile.Stat` reads `CoverSize` from the book
and never touches the lib. The length is 0 because `MakeBook` leaves
`CoverSize` at 0.

### Low: TestNew checks nine of the fourteen root entries

**Fixed.**

`server_test.go`. `by-tag`, `by-status`, `ctl`, `log` and `help` are not in
`wantChildren`, so dropping one from `fs.New` passes.

### Low: TestSingleBookCommandSuccessStrings says "the two commands"

**Fixed.**

`ctl/exec_test.go`. The comment says "The two commands that report on one
book". There is one such command, `delete`, and one subtest.

### Low: an orphaned comment ends match_test.go

**Fixed.**

`views/match_test.go`. Its last three lines describe a fid's query snapshot
and sit on no declaration. The test they belonged to is now
`TestSearchCtlReadsCommittedQuery` in `search_handle_test.go`. AGENTS.md
wants a comment kept on its declaration.

## internal/opds

### Low: TestMissesAre404 includes a case that wants 200

**Fixed.** The 200 case is now `TestUnknownAuthorIsAnEmptyFeed`.

`opds_scenario_test.go`. The table's last row, an author with no books,
expects 200, and the comment above the test is about that row alone. The
name says the opposite.

## internal/testing/fstest

### Low: TestTwoFidsAreIndependent cannot fail

**Fixed.** It runs against `fidFile`.

`fstest_ext_test.go`. It runs against go9p's `StaticFile`, which, as the same
file says further down, "reads on any fid". A clunked sibling fid can never
affect the read, so the test cannot see whether the helper keeps fids apart.
The `fidFile` defined below it would.
