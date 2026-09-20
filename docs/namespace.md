# 9P namespace

The tree `ebookfs` serves. Every path is a synthetic file: reads and writes go
to the library, never to a file on the server's disk of the same name.

```
/
├── books/                       ← flat listing by title
│   └── Title (id)/
│       ├── Title - Author.epub  ← the epub itself, read-only
│       ├── opf                  ← raw package document XML, read-only
│       ├── title                ← read/write
│       ├── authors              ← newline-separated, read/write
│       ├── series               ← read/write
│       ├── series_index         ← read/write
│       ├── language             ← read/write
│       ├── description          ← read/write
│       ├── pubdate              ← read-only
│       ├── identifiers          ← scheme=value per line, read-only
│       ├── cover.jpg            ← read/write, absent when the epub declares
│       │                          no cover, and named for its real extension
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
├── recent/                      ← last 5 books by date_added, newest first
├── search/                      ← clone-style search handles
│   ├── clone                    ← open to allocate a new handle
│   └── <id>/                    ← per-handle directory
│       ├── ctl                  ← write query, read last query, write "close"
│       └── results/             ← matching books (live, not snapshot)
├── inbox/                       ← write here to ingest
├── reader/                      ← export view for rsync-to-Kobo
│   └── Author/                  ← all authors joined with " & "
│       └── Title.epub           ← or .kepub.epub when Convert
├── ctl                          ← write commands, read last result
├── log                          ← recent command history
├── help                         ← command reference
└── stats                        ← read-only aggregate library statistics
```

The epub keeps its real filename, `Title - Author.epub`, or `Title.epub` when
the book has no author.

Writing to a metadata file edits the book. Sidecar fields (`status`, `tags`,
`rating`) update `meta.toml`; bibliographic fields rewrite the epub's package
document. Read `help` on a mounted server for the `ctl` command reference.

See [deployment.md](./deployment.md) for mounting, and [security.md](./security.md)
before exposing the listener: anyone who can connect has full write access to
everything above.
