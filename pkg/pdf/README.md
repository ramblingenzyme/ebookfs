# pkg/pdf

Reads and writes the document metadata of PDF files (ISO 32000-2), in both
places a PDF keeps it: the document information dictionary and the XMP
metadata stream.

## What it does

- Opens PDF files with cross-reference tables, cross-reference streams,
  hybrid references, object streams, incremental updates and linearization
- Reads metadata: title, authors, description, subjects, keywords, language,
  publisher, rights, contributors and creation date
- Writes metadata changes back as an incremental update, leaving every byte of
  the original in place
- Keeps the information dictionary and the XMP packet in agreement

## What it doesn't do

- Encrypted files: `Open` returns `ErrEncrypted`. `OpenFile` still works,
  since the file structure is not encrypted.
- Repairing a broken file: wrong cross-reference offsets are an error, not
  rebuilt by scanning for objects
- Content: no pages, text, fonts or images
- Covers: a PDF has no cover image, only a first page to render
- Per-author sort names and contributor roles: no property holds them.
  calibre's author sort is one string for all authors, reported unsplit.
- LZW, and the image filters (DCT, JPX, CCITT, JBIG2), which never apply to
  the streams it reads
- Creating PDFs from scratch

## Metadata coverage

| Field | Read | Write | Information dictionary | XMP |
|-------|------|-------|------------------------|-----|
| Title | ✓ | ✓ | `Title` | `dc:title` (x-default) |
| Authors | ✓ | ✓ | `Author`, joined by `; ` | `dc:creator` (Seq) |
| Description | ✓ | ✓ | `Subject` | `dc:description` (x-default) |
| Subjects | ✓ | ✓ | | `dc:subject` (Bag) |
| Keywords | ✓ | ✓ | `Keywords` | `pdf:Keywords` |
| Language | ✓ | ✓ | catalog `Lang` | `dc:language` (first) |
| Publisher | ✓ | ✓ | | `dc:publisher` (first) |
| Rights | ✓ | ✓ | | `dc:rights` (x-default) |
| Contributors | ✓ | ✓ | | `dc:contributor` (Bag) |
| Sort title | ✓ | ✓ | | `calibre:title_sort` |
| Author sort | ✓ | ✓ | | `calibre:author_sort`, one string |
| Series | ✓ | ✓ | | `calibre:series`, index in `calibreSI:series_index` |
| Creation date | ✓ | ✗ | `CreationDate` | `xmp:CreateDate` |
| Publication date | ✓ | ✗ | | `dc:date` (first), else PRISM `publicationDate` |
| Identifiers | ✓ | ✗ | | `xmp:Identifier` (scheme in `xmpidq:Scheme`), `dc:identifier`, PRISM `doi`/`isbn` |

Series, sort title and author sort have no standard home in a PDF. They use
calibre's XMP namespace, which calibre writes into every PDF it edits, so a
library migrating from calibre keeps them, and calibre reads what this package
writes. Identifiers are keyed by scheme exactly as `pkg/epub` keys them.

## Choosing between the two sources

Each field comes from XMP, falling back to the information dictionary. The
exception is a dictionary whose `ModDate` is later than the packet's
`xmp:MetadataDate` (or `xmp:ModifyDate`). That means a tool that knew only the
dictionary edited the file last, so the dictionary wins for the fields it has.

The dictionary's `Author` is one string with no defined separator. Read
from there, it is a single author, never split.

## Writing

`Save` does nothing if no field changed since `Open`. Otherwise it writes
every field to each place that disagrees with the `Book`, not only the changed
ones. The new modification dates decide which place a reader trusts, so a
stale value left behind would surface on the next read.

It also:
- sets the dictionary's `ModDate` and the packet's `xmp:ModifyDate` and
  `xmp:MetadataDate` to the same instant (§14.3.4)
- replaces the second file ID, keeping the first (§14.4)
- adds an XMP packet, and points the catalog at it, if the file had none
- writes the update's cross-reference section in the form the file's newest
  section uses, table or stream

## Preservation guarantees

The original file survives byte for byte, followed by the update. Within the
rewritten objects:
- the information dictionary and catalog keep every entry `Save` does not own,
  though keys are re-serialized in sorted order
- the XMP packet keeps every byte outside the properties it replaces: unknown
  namespaces and properties, comments, processing instructions, formatting and
  the packet wrapper. A replaced property moves into one new
  `rdf:Description`.
- for a single-valued field, a language alternative or array whose first item
  already matches is not rewritten, so its other languages and later items
  survive. Changing the first item replaces the whole property.

The package refuses to edit:
- Signed documents (`AcroForm` `SigFlags` bit 1) and certified ones (catalog
  `Perms`). An update would leave the signature's bytes intact, but
  validators would report the document as changed after signing.
- A file whose XMP packet cannot be decoded or parsed, since replacing it
  would lose what it holds
- A field, about to be written to XMP, holding a character XML 1.0 cannot
  represent, such as a control character other than tab, newline or carriage
  return. This includes an unchanged field being synced from the dictionary.

`Save` builds the new file beside the original, reopens it to check that it
reads back the `Book`'s metadata, and renames it into place with the
original's permissions. A failed save leaves the original unchanged. A crash
mid-save can leave the temporary file behind. Its name matches
`SaveTempPattern`, for a caller that sweeps them.

## Size limits

Every length in a PDF is whatever the file claims, so these bound what the
package holds in memory or loops over:

- No stream decodes past 32 MiB.
- At most 1000 cross-reference sections are followed, and a `Prev` chain that
  loops is an error.
- Arrays and dictionaries nest at most 100 deep.
- A chain of references to references is followed at most 32 deep.

## Tolerances

Real files break the spec in common ways, and readers accept them, so this
package does too:

- a header up to 1 KiB into the file, after junk some mailers prepend
- a stream `Length` that is wrong: the data runs to the next `endstream`
- a truncated Flate stream: whatever decoded is kept
- cross-reference table entries 19 bytes long instead of 20
- a missing `endobj`
