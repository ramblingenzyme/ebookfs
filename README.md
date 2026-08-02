# ebookfs

A self-hosted ebook library server that exposes your collection as a synthetic filesystem over the 9P protocol — a lightweight, network-transparent alternative to Calibre.

## Quick Start

```bash
# Build
CGO_ENABLED=0 go build -trimpath -o ebookfs .

# Run
./ebookfs --config config.example.toml

# Mount (on a client machine, via 9pfuse. Other 9p clients may work but only 9pfuse has been tested)
9pfuse tcp!<server-ip>!5640 /mnt/ebookfs

# Browse
ls /mnt/ebookfs/books/
ls /mnt/ebookfs/by-author/
ls /mnt/ebookfs/by-series/

# Ingest a book
cp some-book.epub /mnt/ebookfs/inbox/

# Edit metadata
echo "reading" > /mnt/ebookfs/books/"A Title"/status
echo "4" > /mnt/ebookfs/books/"A Title"/rating

# Search
cat /mnt/ebookfs/search/clone      # allocates a handle, e.g. "0"
echo "author:tolkien+tag:fantasy" > /mnt/ebookfs/search/0/ctl
ls /mnt/ebookfs/search/0/results/

# Bulk operations via the root control file
echo "add-tag favourite author:tolkien" > /mnt/ebookfs/ctl
cat /mnt/ebookfs/ctl               # last command's result
cat /mnt/ebookfs/log               # timestamped history of past commands
cat /mnt/ebookfs/help              # full command reference

# Unmount
sudo umount /mnt/ebookfs
```

## Features

- **9P is the only protocol**
- **Metadata as files** — read/write title, authors, series, tags, status, rating, cover via the filesystem
- **Synthetic inbox** — `cp` an epub into `inbox/`; the server parses, validates, and files it atomically on close
- **Live search** — Plan 9 clone-style API under `search/`: allocate a handle, write a query (`title:`, `author:`, `tag:`, `series:`, `status:`, `id:`, combinable with `+`), read live results back
- **Bulk operations via `ctl`** — a root control file for renaming/merging authors, tags, and series, and for tagging or setting status/rating across many books at once, without a round-trip per book. `log` keeps a timestamped history of past commands and results; `help` documents every command
- **KEPUB conversion** — optional on-the-fly conversion for Kobo e-readers via [kepubify](https://github.com/pgaskin/kepubify)
- **Zero runtime deps** — single static binary, clean ARM cross-compile, ~15 MB Docker image

## Limitations
- No PDF, mobi, cbz support, only epub
- No DRM removal
- No authentication or transport encryption — see [docs/security.md](./docs/security.md) before exposing the server beyond a trusted network
## Bugs
- Editing authors loses third-party metadata (e.g. alternate-script from Calibre/publishers)
- Renaming a series resets all book positions to 1 (doesn't preserve existing index)
- Editing series metadata removes all collections, including sets/bundles (not just series)

## Project goals
- Network transparency
- Filesystem-as-API

### V2
- Encapsulated backend, so `github.com/ramblingenzyme/ebookfs/library` can be used to build other frontends, e.g. OPDS and HTTP
See [ROADMAP.md](./ROADMAP.md) for more details

## Install

```bash
# From source
CGO_ENABLED=0 go build -trimpath -o ebookfs .

# Docker (prebuilt image from GHCR)
docker run -p 5640:5640 -v /path/to/library:/var/lib/ebookfs/library ghcr.io/ramblingenzyme/ebookfs:latest

# Docker (build locally)
docker build -t ebookfs .
docker run -p 5640:5640 -v /path/to/library:/var/lib/ebookfs/library ebookfs
```

See `config.example.toml` for all options.

## Development

```bash
go test ./...
CGO_ENABLED=0 go build -trimpath .
```

## License

MIT
