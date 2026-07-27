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

# Unmount
sudo umount /mnt/ebookfs
```

## Features

- **9P is the only protocol**
- **Metadata as files** — read/write title, authors, series, tags, status, rating, cover via the filesystem
- **Synthetic inbox** — `cp` an epub into `inbox/`; the server parses, validates, and files it atomically on close
- **KEPUB conversion** — optional on-the-fly conversion for Kobo e-readers via [kepubify](https://github.com/pgaskin/kepubify)
- **Zero runtime deps** — single static binary, clean ARM cross-compile, ~15 MB Docker image

## Limitations
- No PDF, mobi, cbz support, only epub
- No DRM removal
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

# Docker
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
