# pkg/epub

Reads and writes metadata in EPUB package documents. Handles both EPUB 2 and EPUB 3 formats.

## What it does

- Opens EPUB files and parses the package document (OPF)
- Reads metadata: title, authors, series, description, language, publisher, rights, subjects, contributors, publication date, identifiers, cover image
- Writes metadata changes back to the EPUB file
- Preserves unknown metadata, namespace declarations, and CDATA sections
- Handles EPUB 2/3 differences transparently (e.g., series encoding, role attributes)

## What it doesn't do

- Structural editing (no adding/removing spine items, manifest items, or content documents)
- Content parsing (no text extraction, no navigation document parsing)
- Format conversion (no EPUB 2 → EPUB 3, no kepub conversion)
- Validation beyond basic archive structure
- Creating EPUBs from scratch

## Metadata coverage

| Field | Read | Write | EPUB 2 | EPUB 3 |
|-------|------|-------|--------|--------|
| Title | ✓ | ✓ | ✓ | ✓ |
| Sort title | ✓ | ✓ | ✓ (calibre:*) | ✓ (file-as) |
| Authors | ✓ | ✓ | ✓ (opf:role) | ✓ (role refinement) |
| Series | ✓ | ✓ | ✓ (calibre:*) | ✓ (belongs-to-collection) |
| Description | ✓ | ✓ | ✓ | ✓ |
| Language | ✓ | ✓ | ✓ | ✓ |
| Publisher | ✓ | ✓ | ✓ | ✓ |
| Rights | ✓ | ✓ | ✓ | ✓ |
| Subjects | ✓ | ✓ | ✓ | ✓ (authority/term) |
| Contributors | ✓ | ✓ | ✓ (opf:role) | ✓ (role refinement) |
| Publication date | ✓ | ✗ | ✓ | ✓ |
| Identifiers | ✓ | ✗ | ✓ | ✓ |
| Cover image | ✓ | ✓ | ✓ | ✓ |

## Preservation guarantees

When writing metadata changes, the package preserves:
- Unknown metadata elements and attributes
- Namespace declarations
- CDATA sections
- Entry order in the ZIP archive
- Modification times and compression methods
- Refinements on elements that aren't being edited

The package refuses to edit:
- Signed EPUBs (would invalidate the signature)
- Encrypted entries (would corrupt them)

## Simplifications

### Repeatable-but-single-valued fields

The EPUB spec allows some Dublin Core elements to repeat (description, language, publisher, rights), but this package treats them as single-valued:

- **Read**: returns the first non-empty element
- **Write**: modifies the primary element; extras survive untouched

This matches the practical reality that most books have one description, one language, one publisher, and one rights statement. The spec allows repetition for edge cases (co-publishers, multi-language descriptions), but those are rare in personal libraries.

### Subject reconciliation

Subjects are repeatable and can carry refinements (authority/term) in EPUB 3. When writing subjects, the package reconciles by text: matching subjects are reused (preserving their refinements), unmatched subjects are removed, and new subjects get new elements.

### Contributor reconciliation

Contributors are reconciled by name, similar to authors. Matching contributors are reused (preserving their role and file-as refinements), unmatched contributors are removed, and new contributors get new elements.
