CREATE TABLE IF NOT EXISTS documents (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id TEXT NOT NULL,
	absolute_path TEXT NOT NULL,
	file_size INTEGER NOT NULL,
	modified_at_ns INTEGER NOT NULL,
	content_hash TEXT,
	scanned_file_size INTEGER,
	scanned_modified_at_ns INTEGER,
	status TEXT NOT NULL DEFAULT 'indexed' CHECK(status IN ('indexed', 'scanned', 'chunked', 'embedded')),
	embedded_content_hash TEXT,
	indexed_at_unix INTEGER,
	deleted_at_unix INTEGER,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(file_id)
);

CREATE TABLE IF NOT EXISTS chunks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	document_id INTEGER NOT NULL,
	chunk_index INTEGER NOT NULL,
	text TEXT NOT NULL,
	token_count INTEGER NOT NULL,
	start_offset INTEGER NOT NULL,
	end_offset INTEGER NOT NULL,
	content_hash TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE,
	UNIQUE(document_id, chunk_index)
);
