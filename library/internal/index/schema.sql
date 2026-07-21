CREATE TABLE books (
    id            INTEGER PRIMARY KEY,
    title         TEXT    NOT NULL,
    sort_title    TEXT,
    pubdate       TEXT,
    description   TEXT    NOT NULL DEFAULT '',
    language      TEXT    NOT NULL DEFAULT '',
    library_path  TEXT    NOT NULL,
    epub_filename TEXT    NOT NULL,
    cover_path    TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'unread',
    rating        REAL    NOT NULL DEFAULT 0, -- validated as 0–5 float (e.g. 4.75) in model.Edits.Validate; rounded to 2dp on write
    date_added    TEXT    NOT NULL,
    date_modified TEXT    NOT NULL,
    series_id     INTEGER REFERENCES series(id),
    series_index  REAL,
    opf_size      INTEGER NOT NULL DEFAULT 0,
    cover_size    INTEGER NOT NULL DEFAULT 0,
    epub_size     INTEGER NOT NULL DEFAULT 0,
    -- Drift bookkeeping: both files' sizes and mtimes as observed by one stat
    -- per file. The epub's name is compared too, via epub_filename above —
    -- rename preserves size and mtime, so those alone cannot see it.
    -- mtimes in Unix nanoseconds because that round-trips a
    -- time.Time losslessly (RFC3339, used for the date columns above, truncates
    -- to whole seconds). Comparison happens in Go, not SQL.
    -- 0 means "never recorded" and reads back as drift.
    -- epub_stat_size duplicates epub_size by value but not by contract:
    -- epub_size is the parser's, and degrades to 0 when the parse-time stat
    -- fails, whereas these four must always come from one successful stat or
    -- the comparison is against a value never actually observed.
    -- TODO: collapse the two. Every write path now requires a successful stat,
    -- so epub_size can be fed from that single observation and stop being
    -- zeroable — which also fixes the 0-length epub reported over 9P and the
    -- guard export sizing needs. See ROADMAP, "single source for the epub's size".
    epub_stat_size INTEGER NOT NULL DEFAULT 0,
    epub_mtime     INTEGER NOT NULL DEFAULT 0,
    meta_mtime     INTEGER NOT NULL DEFAULT 0,
    meta_stat_size INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE authors (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    name      TEXT NOT NULL UNIQUE,
    sort_name TEXT NOT NULL
);

CREATE TABLE book_authors (
    book_id   INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES authors(id),
    position  INTEGER NOT NULL,
    PRIMARY KEY (book_id, author_id)
);

CREATE TABLE series (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE book_tags (
    book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id),
    PRIMARY KEY (book_id, tag_id)
);

CREATE TABLE identifiers (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    scheme  TEXT    NOT NULL,
    value   TEXT    NOT NULL,
    UNIQUE (book_id, scheme)
);

CREATE TABLE book_id_seq (id INTEGER PRIMARY KEY AUTOINCREMENT);

-- Book directories a rebuild walked but could not index (unreadable meta.toml,
-- unparseable epub), with the file state they had at the time. Without this,
-- drift detection sees a directory on disk with no books row and reports drift,
-- so one corrupt epub would reindex the whole library on every startup forever.
-- Recording the file state keeps it self-healing: repair the epub and its mtime
-- changes, which reads as drift and earns the book another indexing attempt.
CREATE TABLE skipped_books (
    library_path   TEXT PRIMARY KEY,
    -- Mirrors books.epub_filename so drift detection can compare the on-disk
    -- epub name for indexed and skipped directories through one code path.
    epub_filename  TEXT NOT NULL DEFAULT '',
    epub_stat_size INTEGER NOT NULL DEFAULT 0,
    epub_mtime     INTEGER NOT NULL DEFAULT 0,
    meta_mtime     INTEGER NOT NULL DEFAULT 0,
    meta_stat_size INTEGER NOT NULL DEFAULT 0
);

-- Each mutation inserts its own row (autocommit, outside the SQL transaction)
-- before touching the store, and deletes it inside the commit transaction.
-- On startup, a non-empty table means an operation may not have completed.
CREATE TABLE pending_ops (
    op_id TEXT PRIMARY KEY
);

CREATE INDEX idx_books_status     ON books(status);
CREATE INDEX idx_books_pubdate    ON books(pubdate);
CREATE INDEX idx_books_date_added ON books(date_added);
CREATE INDEX idx_authors_sort     ON authors(sort_name);
CREATE INDEX idx_book_authors_aid ON book_authors(author_id);
