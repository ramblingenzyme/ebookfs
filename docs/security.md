# Security

`ebookfs` currently ships with **no authentication and no transport encryption**. Read this before exposing the server to anything beyond a machine or network you fully trust.

## Current state

- `server.auth` supports only `"none"` today. `"shared-secret"` exists in the config schema but is rejected at startup with an explicit error (`internal/config/config.go`) rather than silently falling back to unauthenticated. The field is reserved for a future release.
- There is no TLS support anywhere in the codebase. All 9P traffic, including file contents, metadata edits and `ctl` commands, is sent in plain text over TCP.
- The HTTP frontends (OPDS and kosync) are built to run behind a reverse proxy that terminates TLS. ebookfs speaks plain HTTP to the proxy, and the proxy serves HTTPS to clients. Running the HTTP port without one is unsupported beyond a trusted local network.
- The default listen address is `0.0.0.0:5640` (see `configs/config.example.toml`), which binds every network interface on the host, not just loopback.
- The OPDS catalog and kosync progress sync are both off by default. They share one plain-HTTP listener at `http.listen`, which opens only when at least one of them is enabled. The OPDS catalog is unauthenticated, the same as the 9P listener, and the OPDS protocol itself has no write operations.
- kosync checks a username and key on every request except `/sync/healthcheck`. KOReader's protocol fixes the key as the unsalted MD5 of the password, sent in the `X-Auth-Key` header in plain text. `kosync.password` itself sits in plain text in the config file.

## What's at risk

Anyone who can open a TCP connection to the listen port has full read/write access to the entire library: browsing and downloading every book, editing or deleting any book's metadata, running any `ctl` command (including bulk `delete`, `rename-author`, `rename-series`), and ingesting arbitrary files through `inbox/`. There is no concept of a read-only client or a scoped permission; connecting is equivalent to full administrative access.

An enabled OPDS catalog is narrower and still serious: anyone who can reach the HTTP port can browse the catalog and download every book in the library, covers and metadata included. It cannot edit, delete or ingest anything.

Enabled kosync is narrower again, but its credential is easy to take. Anyone who can watch the traffic captures the key from a single request and can replay it indefinitely, since nothing expires it. A captured key also gives up a weak password to an offline MD5 search. A key holder can read and overwrite the reading progress of any book in the library, and can move books from unread to reading to read. It cannot edit other metadata, delete, or ingest.

## Recommendations

- **Don't expose port 5640 to an untrusted network, and never to the public internet.** Treat it the same as you would an unauthenticated NFS or Samba share.
- **Bind to an interface you control.** Set `server.listen` to a loopback or internal-only address (e.g. `127.0.0.1:5640`, or a private VPN/overlay interface) rather than the `0.0.0.0` default, and rely on your network boundary (a firewall, WireGuard, Tailscale, or an SSH tunnel) for anything that needs to reach it from another machine.
- **With Docker**, `-p 5640:5640` publishes the port on every host interface by default. Prefer `-p 127.0.0.1:5640:5640`, or omit the publish entirely and put the container on a private network shared only with trusted hosts.
- **Set `http.base_url`** whenever the catalog is reachable from outside the host. Without it, the absolute URLs in served documents are built from the client's own `Host` and `X-Forwarded-*` headers, which a caller controls. That is a cache-poisoning vector wherever a shared cache sits in front. Behind a proxy, it is the proxy's public `https://` URL.
- **Serve HTTP through a TLS-terminating reverse proxy** (Caddy, nginx, Traefik). Bind `http.listen` to loopback or a private interface, so the proxy is the only way in. kosync depends on the proxy most, since without TLS every request hands out a replayable key. KOReader accepts an `https://` server URL.
- **Give kosync a password used nowhere else**, and keep the config file readable only by the account `ebookfs` runs as.
- **For local tooling** (downloaders, scripts, cron jobs) that need to drop files into `inbox/`, use the loopback-mount pattern in [deployment.md](./deployment.md#local-loopback-mount-on-the-server-host) instead of pointing them at the network listener. It reaches the server the same way a remote client would, without leaving the host.
- Process hardening limits what a compromised `ebookfs` can do to the host. It does not restrict who can connect over the network.
