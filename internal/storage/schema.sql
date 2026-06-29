CREATE TABLE IF NOT EXISTS documents (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	root_path TEXT NOT NULL,
	relative_path TEXT NOT NULL,
	absolute_path TEXT NOT NULL,
	file_size INTEGER NOT NULL,
	modified_at_ns INTEGER NOT NULL,
	content_hash TEXT,
	indexed_at_unix INTEGER,
	deleted_at_unix INTEGER,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(root_path, relative_path)
);
