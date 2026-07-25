-- Mutation operations

-- name: InsertBook :exec
INSERT INTO books (
    id, title, sort_title, pubdate, description, language,
    library_path, epub_filename, cover_path, status, rating,
    date_added, date_modified, series_id, series_index,
    opf_size, cover_size, epub_size, epub_mtime, meta_mtime, meta_size
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpsertBook :exec
INSERT INTO books (
    id, title, sort_title, pubdate, description, language,
    library_path, epub_filename, cover_path, status, rating,
    date_added, date_modified, series_id, series_index,
    opf_size, cover_size, epub_size, epub_mtime, meta_mtime, meta_size
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title=excluded.title, sort_title=excluded.sort_title, pubdate=excluded.pubdate,
    description=excluded.description, language=excluded.language,
    library_path=excluded.library_path, epub_filename=excluded.epub_filename,
    cover_path=excluded.cover_path, status=excluded.status, rating=excluded.rating,
    date_added=excluded.date_added, date_modified=excluded.date_modified,
    opf_size=excluded.opf_size, cover_size=excluded.cover_size,
    epub_size=excluded.epub_size, epub_mtime=excluded.epub_mtime,
    meta_mtime=excluded.meta_mtime, meta_size=excluded.meta_size;

-- name: DeleteBook :exec
DELETE FROM books WHERE id = ?;

-- Author operations

-- name: InsertAuthor :exec
INSERT INTO authors (name, sort_name) VALUES (?, ?)
ON CONFLICT(name) DO UPDATE SET sort_name = excluded.sort_name
WHERE excluded.sort_name != '' AND authors.sort_name = '';

-- name: GetAuthorByName :one
SELECT * FROM authors WHERE name = ?;

-- name: DeleteBookAuthors :exec
DELETE FROM book_authors WHERE book_id = ?;

-- name: InsertBookAuthor :exec
INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?);

-- Tag operations

-- name: InsertTag :exec
INSERT OR IGNORE INTO tags (name) VALUES (?);

-- name: GetTagByName :one
SELECT * FROM tags WHERE name = ?;

-- name: DeleteBookTags :exec
DELETE FROM book_tags WHERE book_id = ?;

-- name: InsertBookTag :exec
INSERT INTO book_tags (book_id, tag_id) VALUES (?, ?);

-- Series operations

-- name: InsertSeries :exec
INSERT OR IGNORE INTO series (name) VALUES (?);

-- name: GetSeriesByName :one
SELECT * FROM series WHERE name = ?;

-- name: UpdateBookSeries :exec
UPDATE books SET series_id = ?, series_index = ? WHERE id = ?;

-- Identifier operations

-- name: DeleteBookIdentifiers :exec
DELETE FROM identifiers WHERE book_id = ?;

-- name: InsertIdentifier :exec
INSERT INTO identifiers (book_id, scheme, value) VALUES (?, ?, ?);

-- Relationship loading

-- name: GetAuthorsByBookID :many
SELECT a.* FROM authors a
JOIN book_authors ba ON a.id = ba.author_id
WHERE ba.book_id = ?
ORDER BY ba.position;

-- name: GetAuthorsByBookIDs :many
SELECT ba.book_id, a.id, a.name, a.sort_name
FROM book_authors ba
JOIN authors a ON a.id = ba.author_id
WHERE ba.book_id IN (sqlc.slice('book_ids'))
ORDER BY ba.book_id, ba.position;

-- name: GetTagsByBookID :many
SELECT t.name FROM tags t
JOIN book_tags bt ON t.id = bt.tag_id
WHERE bt.book_id = ?
ORDER BY t.name;

-- name: GetTagsByBookIDs :many
SELECT bt.book_id, t.name
FROM book_tags bt
JOIN tags t ON t.id = bt.tag_id
WHERE bt.book_id IN (sqlc.slice('book_ids'))
ORDER BY bt.book_id, t.name;

-- name: GetIdentifiersByBookID :many
SELECT scheme, value FROM identifiers WHERE book_id = ?;

-- name: GetIdentifiersByBookIDs :many
SELECT book_id, scheme, value
FROM identifiers
WHERE book_id IN (sqlc.slice('book_ids'))
ORDER BY book_id;

-- Stats operations

-- name: GetStats :one
SELECT
    COUNT(*) AS books,
    CAST(COALESCE(SUM(epub_size), 0) AS INTEGER) AS total_size,
    MAX(date_added) AS last_added,
    MAX(date_modified) AS last_modified,
    (SELECT COUNT(*) FROM authors) AS authors,
    (SELECT COUNT(*) FROM series) AS series,
    (SELECT COUNT(*) FROM tags) AS tags
FROM books;

-- Drift detection

-- name: GetAllPathInfo :many
SELECT library_path, epub_filename, epub_size, epub_mtime, meta_mtime, meta_size FROM books
UNION ALL
SELECT library_path, epub_filename, epub_size, epub_mtime, meta_mtime, meta_size FROM skipped_books;

-- Cleanup

-- name: DeleteOrphanedAuthors :exec
DELETE FROM authors WHERE id NOT IN (SELECT author_id FROM book_authors);

-- name: DeleteOrphanedSeries :exec
DELETE FROM series WHERE id NOT IN (SELECT series_id FROM books WHERE series_id IS NOT NULL);

-- name: DeleteOrphanedTags :exec
DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM book_tags);

-- Pending operations

-- name: InsertPendingOp :exec
INSERT INTO pending_ops (op_id) VALUES (?);

-- name: DeletePendingOp :exec
DELETE FROM pending_ops WHERE op_id = ?;

-- name: DeleteAllPendingOps :exec
DELETE FROM pending_ops;

-- name: CountPendingOps :one
SELECT COUNT(*) FROM pending_ops;

-- ID sequence operations

-- name: NextBookID :one
INSERT INTO book_id_seq DEFAULT VALUES RETURNING id;

-- name: InsertSkippedBook :exec
INSERT INTO skipped_books (library_path, epub_filename, epub_size, epub_mtime, meta_mtime, meta_size)
VALUES (?, ?, ?, ?, ?, ?);

-- name: SetBookIDSequence :exec
INSERT INTO book_id_seq(id) VALUES(?) ON CONFLICT(id) DO NOTHING;

-- ListAuthors

-- name: ListAuthors :many
SELECT * FROM authors ORDER BY sort_name;
