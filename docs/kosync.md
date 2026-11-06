# Kosync: Reading Progress Synchronization

Kosync is a progress synchronization protocol used by KOReader to sync reading positions across devices. ebookfs implements a kosync-compatible server that integrates with your library to automatically track reading progress and update book statuses.

## Overview

When enabled, ebookfs provides a kosync server at `/sync/` that allows KOReader devices to:
- Sync reading progress (current position and percentage)
- Automatically update book statuses based on reading progress
- Resume reading from the last position across devices

The implementation stores progress data in per-book sidecars and keeps an in-memory index of document IDs to book IDs, rebuilt from the sidecars at every startup.

## Configuration

Enable kosync by adding credentials to your config file:

```toml
[kosync]
enable = true
username = "myuser"
password = "mypassword"
reading_threshold = 0.05
read_threshold = 0.95
```

### Credentials

Kosync uses pre-provisioned credentials only. User registration is disabled.

- **username**: The username KOReader will use to authenticate
- **password**: The password in plaintext. ebookfs hashes it to MD5 at startup, which is what KOReader sends

Configure the same username and password in KOReader's kosync plugin settings.

### Status Thresholds

ebookfs automatically updates book statuses based on reading progress:

- **reading_threshold** (default: 0.05 = 5%): When progress reaches this percentage, the book status changes from "unread" to "reading"
- **read_threshold** (default: 0.95 = 95%): When progress reaches this percentage, the book status changes to "read"

Status transitions are one-way: once a book is marked "read", it will not be downgraded to "reading" or "unread" even if progress decreases.

## Storage Layout

Kosync data is stored in one location:

1. **Per-book sidecars**: `{library_root}/{author}/{title} ({id})/.sidecar/kosync.json`
   - Contains the document ID(s) and current progress
   - Document IDs are computed using a partial MD5 algorithm matching KOReader's specification

The document-ID to book-ID index used for lookup is a separate in-memory map, not persisted anywhere.

## Index Recovery

The index is rebuilt from the sidecars at every startup. That scan costs one small read per book, which is why the index is not persisted — a second copy of the document IDs would drift from the sidecars it derives from. If startup restore ever becomes slow at a large library, a persisted snapshot of the index is the upgrade path. During the rebuild:
- All books in the library are scanned
- Each book's kosync sidecar is read to extract document IDs
- Books without a sidecar get one created (the document ID is computed from the epub)
- Books with corrupt sidecars are skipped (logged as warnings)

## Protocol Compatibility

The implementation follows the kosync protocol specification:

- **Endpoints**: `/sync/healthcheck`, `/sync/users/auth`, `/sync/syncs/progress`
- **Authentication**: HTTP headers `X-Auth-User` and `X-Auth-Key`
- **Document ID**: Partial MD5 algorithm (12 samples at geometric offsets) matching KOReader's `partial_md5_checksum`

KOReader clients should work without modification when configured with the ebookfs server URL.

## Troubleshooting

### Kosync not working

1. **Check logs**: Look for "kosync: computed document ID" messages during book ingest
2. **Verify credentials**: Ensure `enable = true`, and that username and password match in both ebookfs config and KOReader
3. **Check index**: Look for "kosync: index restored" in logs
4. **Verify sidecars**: Check that `{library_root}/{author}/{title} ({id})/.sidecar/kosync.json` exists for books you're reading

### Progress not syncing between devices

1. Ensure both devices are configured with the same ebookfs server URL
2. Verify the same username and password are used on both devices
3. Check that the book's document ID matches (visible in the sidecar file)
4. Look for "kosync: updated book status" messages in logs when progress is received

### Document ID mismatch

If KOReader computes a different document ID than ebookfs:
- The book file may have been modified after ingest
- Re-ingest the book to recompute the document ID
- The index will be updated automatically

### Corrupt sidecar

If a sidecar becomes corrupted:
1. The book will be skipped during index restore (warning logged)
2. Progress for that book will not sync until the sidecar is fixed
3. Re-ingesting the book will recreate the sidecar (but lose existing progress)

## Limitations

- Single user only: kosync supports one pre-provisioned account per ebookfs instance
- No encryption: progress data is stored in plain JSON files
- No conflict resolution: last write wins (matches kosync protocol specification)
