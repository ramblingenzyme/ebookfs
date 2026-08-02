# Security

`ebookfs` currently ships with **no authentication and no transport encryption**. Read this before exposing the server to anything beyond a machine or network you fully trust.

## Current state

- `server.auth` supports only `"none"` today. `"shared-secret"` exists in the config schema but is rejected at startup with an explicit error (`library/config/config.go`) rather than silently falling back to unauthenticated — the field is reserved for a future release, not a usable option yet.
- There is no TLS support anywhere in the codebase. All 9P traffic — including file contents, metadata edits, and `ctl` commands — is sent in plain text over TCP.
- The default listen address is `0.0.0.0:5640` (see `config.example.toml`), which binds every network interface on the host, not just loopback.

## What's at risk

Anyone who can open a TCP connection to the listen port has full read/write access to the entire library: browsing and downloading every book, editing or deleting any book's metadata, running any `ctl` command (including bulk `delete`, `rename-author`, `rename-series`), and ingesting arbitrary files through `inbox/`. There is no concept of a read-only client or a scoped permission — connecting is equivalent to full administrative access.

## Recommendations

- **Don't expose port 5640 to an untrusted network, and never to the public internet.** Treat it the same as you would an unauthenticated NFS or Samba share.
- **Bind to an interface you control.** Set `server.listen` to a loopback or internal-only address (e.g. `127.0.0.1:5640`, or a private VPN/overlay interface) rather than the `0.0.0.0` default, and rely on your network boundary — a firewall, WireGuard, Tailscale, or an SSH tunnel — for anything that needs to reach it from another machine.
- **With Docker**, `-p 5640:5640` publishes the port on every host interface by default. Prefer `-p 127.0.0.1:5640:5640`, or omit the publish entirely and put the container on a private network shared only with trusted hosts.
- **For local tooling** (downloaders, scripts, cron jobs) that need to drop files into `inbox/`, use the loopback-mount pattern in [deployment.md](./deployment.md#local-loopback-mount-on-the-server-host) instead of pointing them at the network listener — it reaches the server the same way a remote client would, but never leaves the host.
- The systemd hardening in deployment.md (`NoNewPrivileges`, `ProtectSystem=strict`, etc.) limits what a compromised `ebookfs` process can do to the rest of the host — it does not restrict who can connect to it over the network. Network exposure is a separate concern from process hardening, and both matter.

## Roadmap

Shared-secret authentication is planned but not yet implemented — config validation deliberately refuses to start with `auth = "shared-secret"` rather than accept it and serve unauthenticated. Until it lands, network boundary controls (firewall rules, VPN, SSH tunnel) are the only available mitigation.
