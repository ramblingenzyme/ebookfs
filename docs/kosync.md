# Kosync: Reading Progress Synchronization

Kosync is a progress synchronization protocol used by KOReader to sync reading positions across devices. ebookfs implements a kosync-compatible server that integrates with your library to automatically track reading progress and update book statuses.

## Overview

When enabled, ebookfs provides a kosync server at `/sync/` that allows KOReader devices to:
- Sync reading progress (current position and percentage)
- Automatically update book statuses based on reading progress
- Resume reading from the last position across devices

The implementation stores progress data in per-book sidecars and maintains a mapping document IDs to book IDs for efficient lookup.

## Configuration

Enable kosync by adding credentials to your config file:

```toml
[kosync]
username = "myuser"
credential = "5f4dcc3b5aa765d61d8327deb882cf99"
reading_threshold = 0.05
read_threshold = 0.95
```

### Credentials

Kosync uses pre-provisioned credentials only. User registration is disabled.

- **username**: The username KOReader will use to authenticate
- **credential**: The MD5 hash of the password (lowercase hex)

To generate the credential:

```bash
# Using md5sum
echo -n "mypassword" | md5sum | cut -d' ' -f1

# Using Python
python3 -c 'import hashlib; print(hashlib.md5(b"mypassword").hexdigest())'
```

Configure the same username and password in KOReader's kosync plugin settings.

### Status Thresholds

ebookfs automatically updates book statuses based on reading progress:

- **reading_threshold** (default: 0.05 = 5%): When progress reaches this percentage, the book status changes from "unread" to "reading"
- **read_threshold** (default: 0.95 = 95%): When progress reaches this percentage, the book status changes to "read"

Status transitions are one-way: once a book is marked "read", it will not be downgraded to "reading" or "unread" even if progress decreases.

## Storage Layout

Kosync data is stored in two locations:

1. **Per-book sidecars**: `{library_root}/books/{book_dir}/.sidecar/kosync.json`
   - Contains the document ID(s) and current progress
   - Document IDs are computed using a partial MD5 algorithm matching KOReader's specification

2. **Mapping file**: `{library_root}/.kosync/mapping.json`
   - Maps document IDs to book IDs for efficient lookup
   - Can be rebuilt from sidecars if lost or corrupted

## Mapping Recovery

The mapping file is a derived index that can be rebuilt from book sidecars. ebookfs automatically rebuilds the mapping on startup if it's missing or corrupted.

Manual rebuild is not typically necessary, but if needed, restarting ebookfs will trigger the rebuild process. During rebuild:
- All books in the library are scanned
- Each book's kosync sidecar is read to extract document IDs
- The mapping is reconstructed atomically
- Books without sidecars or with corrupt sidecars are skipped (logged as warnings)

## Protocol Compatibility

The implementation follows the kosync protocol specification:

- **Endpoints**: `/sync/healthcheck`, `/sync/users/auth`, `/sync/syncs/progress`
- **Authentication**: HTTP headers `X-Auth-User` and `X-Auth-Key`
- **Document ID**: Partial MD5 algorithm (12 samples at geometric offsets) matching KOReader's `partial_md5_checksum`

KOReader clients should work without modification when configured with the ebookfs server URL.

## Troubleshooting

### Kosync not working

1. **Check logs**: Look for "kosync: computed document ID" messages during book ingest
2. **Verify credentials**: Ensure username and credential (MD5 hash) are correct in both ebookfs config and KOReader
3. **Check mapping**: Look for "kosync: mapping rebuilt" in logs if the mapping was missing or corrupted
4. **Verify sidecars**: Check that `{library_root}/books/{book_dir}/.sidecar/kosync.json` exists for books you're reading

### Progress not syncing between devices

1. Ensure both devices are configured with the same ebookfs server URL
2. Verify the same username and password are used on both devices
3. Check that the book's document ID matches (visible in the sidecar file)
4. Look for "kosync: updated book status" messages in logs when progress is received

### Document ID mismatch

If KOReader computes a different document ID than ebookfs:
- The book file may have been modified after ingest
- Re-ingest the book to recompute the document ID
- The mapping will be updated automatically

### Corrupt sidecar

If a sidecar becomes corrupted:
1. The book will be skipped during mapping rebuild (warning logged)
2. Progress for that book will not sync until the sidecar is fixed
3. Re-ingesting the book will recreate the sidecar (but lose existing progress)

## Limitations

- Single user only: kosync supports one pre-provisioned account per ebookfs instance
- No encryption: progress data is stored in plain JSON files
- No conflict resolution: last write wins (matches kosync protocol specification)
