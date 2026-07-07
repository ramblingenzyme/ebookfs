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
    epub_size     INTEGER NOT NULL DEFAULT 0
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

-- Each mutation inserts its own row (autocommit, outside the SQL transaction)
-- before touching the store, and deletes it inside the commit transaction.
-- On startup, a non-empty table means an operation may not have completed.
CREATE TABLE pending_ops (
    op_id      TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_books_status     ON books(status);
CREATE INDEX idx_books_pubdate    ON books(pubdate);
CREATE INDEX idx_books_date_added ON books(date_added);
CREATE INDEX idx_authors_sort     ON authors(sort_name);
CREATE INDEX idx_book_authors_aid ON book_authors(author_id);
