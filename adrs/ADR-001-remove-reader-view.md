# ADR-001: Remove reader/ view and export abstractions

## Status
accepted

## Context
The `reader/` 9P view served as an export mechanism for syncing books to e-readers via rsync. It supported optional KEPUB conversion through the `Exporter` interface (with implementations for epub and kepub formats). The OPDS frontend used a similar `Renderer` interface for serving book content.

Two problems emerged with this design:

1. **Filename instability**: When book metadata changed (e.g., author names, title corrections), the exported filename in `reader/` would change, orphaning any progress or annotations stored against the old filename on the device. This made `reader/` unreliable as a stable rsync target.

2. **Format rigidity**: The `Exporter` abstraction hardcoded epub and kepub support. Adding new formats required extending the interface and all implementations, creating friction for format experimentation.

Meanwhile, OPDS matured as a more robust device sync mechanism. Unlike `reader/`, OPDS serves books by ID rather than by derived filename, so metadata changes don't break the sync relationship. The device maintains a stable reference to each book regardless of how its display metadata evolves.

## Decision
Remove the following from the codebase:

- The `reader/` 9P view and its associated view implementation
- The `Exporter` interface and its implementations (epub exporter, kepub cache)
- The `Renderer` interface from OPDS
- The `ReaderConfig` configuration (statuses filter, convert flag)
- The `OPDSConfig.Convert` flag
- All KEPUB conversion logic and dependencies

OPDS now serves the original epub files directly from the library, using the `Library.Content()` method to access book content. The `reader/` directory is removed from the 9P namespace.

Device sync is now handled exclusively through OPDS, which provides stable book references and eliminates the filename instability problem.

## Consequences

**Good:**
- Simpler codebase with fewer abstraction layers
- No KEPUB conversion overhead or caching complexity
- Stable device sync through OPDS (book IDs don't change with metadata)
- Easier to add new export formats in the future if needed (no interface to extend)
- Reduced configuration surface (no `[reader]` section, no `opds.convert` flag)

**Bad:**
- Users who relied on `reader/` for rsync-based sync must migrate to OPDS
- KEPUB conversion is no longer available (users must rely on device-side conversion or find alternative workflows)
- Loss of the ability to filter exported books by reading status (OPDS serves all books)

**Mitigations:**
- OPDS provides equivalent or better functionality for device sync
- KEPUB conversion, if needed, can be handled by the reading device or external tools
- The removal is a breaking change for workflows depending on `reader/`, but the affected user base is expected to be small
