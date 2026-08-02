# Deployment

How to run `ebookfs` as a long-lived service and mount it, locally and over the network.

## Prerequisites

`ebookfs` has no runtime dependencies. The binary is statically compiled with `CGO_ENABLED=0` and runs on any Linux system regardless of installed packages. KEPUB conversion is linked in at build time (`kepubify/v4`) — no external tools are needed at runtime.

```bash
# Native build
CGO_ENABLED=0 go build -trimpath -o ebookfs .

# Cross-compile for an ARM server
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o ebookfs .
```

Config lives at `/etc/ebookfs/config.toml` (or wherever `--config` points). See `config.example.toml` for all options. `library.inbox_temp` must be on the same filesystem as `library.root` so ingestion can finalize with `rename(2)`.

Before deciding what `server.listen` should bind to and whether to publish the port, read [security.md](./security.md) — the server has no authentication or transport encryption today.

## Mounting from a client

`9pfuse` is the tested client:

```bash
9pfuse tcp!<server-ip>!5640 /mnt/ebookfs
```

The kernel `v9fs` client should also work but is untested. The server speaks plain 9P2000 (not `9p2000.L` or `9p2000.u`), so the mount must say so:

```bash
mount -t 9p -o trans=tcp,port=5640,version=9p2000 <server-ip> /mnt/ebookfs
```

Or in `/etc/fstab`:

```
<server-ip>  /mnt/ebookfs  9p  trans=tcp,port=5640,version=9p2000,_netdev,nofail  0  0
```

`nofail` and `_netdev` let the client boot even when the server is unreachable.

## Local loopback mount on the server host

The server host can mount its own 9P export over loopback so that any local process interacts with the library through ordinary filesystem operations. This is what lets external tools (LazyLibrarian, scripts, cron jobs) drop files into `inbox/` without speaking 9P themselves.

With the filesystem mounted at `/mnt/ebookfs`, a downloader's post-processing destination becomes `/mnt/ebookfs/inbox/`. A `cp` into that directory translates to `Tcreate`+`Twrite`+`Tclunk` on the loopback connection, triggering the same ingestion pipeline as a remote write.

## Docker

See the README for the Docker build and run commands. The container listens on 5640 and expects the library volume at `/var/lib/ebookfs/library`.

## Backups

The library is plain files and the SQLite index is a derived cache, rebuilt from the filesystem on every start. Any file-based backup tool pointed at `library.root` captures everything; `.index.db` and `.inbox-tmp/` can be excluded.
