CREATE TABLE IF NOT EXISTS documents (
	id BIGSERIAL PRIMARY KEY,
	file_id TEXT NOT NULL,
	absolute_path TEXT NOT NULL,
	file_size BIGINT NOT NULL,
	modified_at_ns BIGINT NOT NULL,
	content_hash TEXT,
	scanned_file_size BIGINT,
	scanned_modified_at_ns BIGINT,
	status TEXT NOT NULL DEFAULT 'indexed' CHECK (status IN ('indexed', 'scanned', 'chunked', 'embedded')),
	embedded_content_hash TEXT,
	indexed_at_unix BIGINT,
	deleted_at_unix BIGINT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (file_id)
);

CREATE TABLE IF NOT EXISTS chunks (
	id BIGSERIAL PRIMARY KEY,
	document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
	chunk_index BIGINT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL,
	token_count BIGINT NOT NULL,
	start_offset BIGINT NOT NULL,
	end_offset BIGINT NOT NULL,
	content_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (document_id, chunk_index)
);
