# Go CLI Markdown Vector Indexer

## 1. Purpose

Implement a local Go CLI application that recursively scans a directory, reads Markdown files, splits each document into chunks, sends each chunk to a remote embedding endpoint exposed by LM Studio, and stores the resulting vectors in a local SQLite database using the Vectorlite extension.

The CLI has two primary operations:

1. **Index** — crawl a directory and insert or update Markdown documents and their chunks.
2. **Search** — embed a query, search the chunk vectors, group results by document, and return the best matching documents with their strongest matching chunks.

The application must run locally and must not require a database server.

Every Go source file must have a corresponding unit test.

---

## 2. Scope

### Included

- Recursive directory crawling
- Markdown file discovery
- UTF-8 text reading
- Deterministic chunking without an LLM
- Token-aware maximum chunk size
- Configurable chunk overlap
- Remote embedding calls through LM Studio
- SQLite persistence
- Vectorlite-based vector search
- Incremental reindexing
- Deleted-file cleanup
- Search grouped by document
- JSON and human-readable output
- Clear logging and error reporting

### Excluded

- PDF, DOCX, HTML, and image extraction
- OCR
- LLM-based summarization
- Reranking with a cross-encoder or LLM
- File watching
- Background daemon mode
- Multi-user or network server mode
- Distributed indexing
- Cloud-hosted vector databases

---

## 3. High-Level Architecture

```text
                       ┌─────────────────────────┐
                       │       Go CLI            │
                       └────────────┬────────────┘
                                    │
                    ┌───────────────┴────────────────┐
                    │                                │
              index command                    search command
                    │                                │
                    v                                v
          recursive Markdown crawl          embed search query
                    │                                │
                    v                                v
          deterministic chunking             Vectorlite ANN search
                    │                                │
                    v                                v
          LM Studio embedding API       group candidates by document
                    │                                │
                    v                                v
       SQLite + Vectorlite persistence      return ranked documents
```

Recommended internal modules:

```text
cmd/
  root.go
  index.go
  search.go

internal/
  config/
  crawler/
  reader/
  parser/
  chunker/
  strategy/
  tokenizer/
  embedder/
  storage/
  vectorstore/
  indexer/
  search/
  output/

main.go
```

---

## 4. CLI Interface

Use a mature CLI framework such as Cobra.

Recommended dependency:

```text
github.com/spf13/cobra
```

### 4.1 Index command

```bash
vector-cli index /path/to/documents
```

Example:

```bash
vector-cli index ~/Documents/knowledge \
  --db ~/.local/share/vector-cli/index.db \
  --embedding-url http://127.0.0.1:1234/v1/embeddings \
  --embedding-model text-embedding-model \
  --max-tokens 500 \
  --overlap-tokens 50
```

Recommended flags:

```text
--db                 SQLite database path
--vector             Path to the Vectorlite native extension
--embedding-url      LM Studio embeddings endpoint
--embedding-model    Model identifier sent to LM Studio
--max-tokens         Maximum tokens per chunk
--overlap-tokens     Approximate overlap between adjacent chunks
--batch-size         Number of texts sent per embedding request
--include-hidden     Include hidden directories and files
--follow-symlinks    Follow symbolic links
--full-reindex       Delete and rebuild all indexed data
--remove-missing     Remove records for files no longer present
--json               Emit machine-readable output
--verbose            Enable detailed logs
```

### 4.2 Search command

```bash
vector-cli search "how is payment configuration handled"
```

Example:

```bash
vector-cli search "how is payment configuration handled" \
  --db ~/.local/share/vector-cli/index.db \
  --embedding-url http://127.0.0.1:1234/v1/embeddings \
  --embedding-model text-embedding-model \
  --limit 10 \
  --candidate-chunks 100 \
  --chunks-per-document 3
```

Recommended flags:

```text
--db                   SQLite database path
--vector               Path to the Vectorlite native extension
--embedding-url        LM Studio embeddings endpoint
--embedding-model      Model identifier
--limit                 Number of documents to return
--candidate-chunks      Number of chunk candidates retrieved before grouping
--chunks-per-document   Maximum matching chunks returned per document
--path-prefix           Restrict search to indexed paths under a prefix
--min-score             Optional minimum similarity threshold
--json                  Emit machine-readable output
```

---

## 5. Configuration

Support configuration through:

1. Command-line flags
2. Environment variables
3. Optional config file

Suggested environment variables:

```text
VECTOR_CLI_DB
VECTOR_CLI_VECTORLITE_PATH
VECTOR_CLI_EMBEDDING_URL
VECTOR_CLI_EMBEDDING_MODEL
VECTOR_CLI_MAX_TOKENS
VECTOR_CLI_OVERLAP_TOKENS
VECTOR_CLI_BATCH_SIZE
```

Precedence:

```text
CLI flags > environment variables > config file > defaults
```

Suggested defaults:

```text
database:             ./vector-index.db
embedding URL:        http://127.0.0.1:1234/v1/embeddings
embedding dimensions: 768
max tokens:           500
overlap tokens:       50
embedding batch size: 16
candidate chunks:     100
result documents:     10
chunks per document:  3
```

---

## 6. SQLite and Vectorlite Integration

Use:

```text
github.com/mattn/go-sqlite3
```

The application must load Vectorlite as a SQLite extension.

Conceptual initialization:

```go
sql.Register("sqlite3_vectorlite", &sqlite3.SQLiteDriver{
    Extensions: []string{
        vectorPath,
    },
})

db, err := sql.Open("sqlite3_vectorlite", databasePath)
```

Requirements:

- CGO must be enabled.
- The Vectorlite extension must match the current OS and architecture.
- The extension path must be configurable.
- Database initialization must verify that Vectorlite loaded successfully.
- Startup must fail with a clear error when the extension is missing or incompatible.

Run a small health query after opening the database.

The exact Vectorlite health function and virtual-table syntax must be confirmed against the bundled Vectorlite version. Keep all Vectorlite-specific SQL isolated inside the `vectorstore` package so that syntax changes do not affect the rest of the application.

---

## 7. Data Model

Use regular SQLite tables for documents, chunks, indexing metadata, and configuration.

Use a Vectorlite virtual table for the chunk vectors.

### 7.1 Documents

```sql
CREATE TABLE IF NOT EXISTS documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id TEXT NOT NULL,
    absolute_path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    modified_at_ns INTEGER NOT NULL,
    content_hash TEXT,
    scanned_file_size INTEGER,
    scanned_modified_at_ns INTEGER,
    status TEXT NOT NULL DEFAULT 'indexed'
        CHECK(status IN ('indexed', 'scanned', 'chunked', 'embedded', 'done', 'failed')),
    title TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(file_id)
);
```

### 7.2 Chunks

```sql
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
```

### 7.3 Index metadata

```sql
CREATE TABLE IF NOT EXISTS index_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

Store at least:

```text
schema_version
distance_metric
chunk_max_tokens
chunk_overlap_tokens
```

### 7.4 Vector table

Create one Vectorlite row per chunk. Do not store vectors in a normal SQLite table.

Conceptually:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS chunk_vectors
USING vectorlite(
    embedding float32[EMBEDDING_DIMENSIONS],
    hnsw(max_elements=EXPECTED_MAX_CHUNKS)
);
```

The row identifier must map directly to `chunks.id`.

The exact insert and search syntax depends on the installed Vectorlite version. Encapsulate it behind a Go interface:

```go
type VectorStore interface {
    Insert(ctx context.Context, chunkID int64, vector []float32) error
    Delete(ctx context.Context, chunkIDs []int64) error
    Search(ctx context.Context, query []float32, limit int) ([]VectorHit, error)
}
```

Do not spread raw Vectorlite SQL across command handlers.

---

## 8. Markdown Crawling

### 8.1 Recursive discovery

Use `filepath.WalkDir`.

Only index files with these extensions:

```text
.md
.markdown
```

Extension matching must be case-insensitive.

Skip by default:

```text
.git
.hg
.svn
node_modules
vendor
dist
build
.cache
.Trash
```

Skip hidden files and directories unless `--include-hidden` is set.

Do not follow symlinks by default.

Prevent duplicate indexing when symlinks or alternate paths resolve to the same physical file.

File-type support is injected as a strategy pool object. Each file strategy declares which paths it supports and composes reader, parser, and chunker implementations from the internal packages. The index stage must skip files that are not supported by the injected strategy pool. The initial default strategy pool supports Markdown files only.

### 8.2 File identity

A document is identified by:

```text
file_id
```

Use the filesystem identity derived from device ID and inode when available.

Store the current absolute path for convenience.

### 8.3 File change detection

Before reading and embedding a known file, compare:

```text
file size
modification time
content hash
```

Recommended flow:

1. Compare size and modification timestamp.
2. If unchanged, skip the file.
3. If changed, read the file and compute SHA-256.
4. If the hash is unchanged, update metadata but do not re-embed.
5. If the hash changed, delete old chunks and vectors, then rebuild.

Use SHA-256 for deterministic content hashes.

---

## 9. Markdown Text Preparation

The Markdown source should remain mostly intact because headings, lists, and code blocks may carry semantic meaning.

Perform only conservative normalization:

- Normalize line endings to `\n`.
- Remove an optional UTF-8 BOM.
- Trim excessive leading and trailing blank lines.
- Collapse more than three consecutive blank lines.
- Preserve headings.
- Preserve list items.
- Preserve fenced code blocks.
- Preserve inline code.
- Do not render Markdown to HTML before embedding.
- Do not remove all Markdown punctuation unless benchmarks prove it helps.

Optional enhancement:

Prepend a document title and the nearest heading path to each chunk before embedding.

Example embedding input:

```text
Document: Payment Configuration

Section: Checkout > Payment Providers

The payment provider can be configured through...
```

The stored `chunks.text` may contain only the original chunk text, while the embedding input may contain this added context.

---

## 10. Deterministic Chunking

Chunking must not use an LLM.

### 10.1 Requirements

- Maximum of 300 estimated tokens by default in the initial implementation.
- Configurable token overlap.
- Prefer natural Markdown boundaries.
- Do not split a sentence unless the sentence itself exceeds the token limit.
- Do not split a fenced code block unless it exceeds the token limit.
- Preserve chunk order.
- Produce deterministic output for identical input and configuration.

The initial implementation uses a replaceable chunking strategy interface and a basic hard-limit chunker. It estimates token count from average token length and cuts text at the configured token budget. More advanced Markdown-aware strategies can replace this implementation later without changing the pipeline shape.

Chunking can be file-type dependent. File strategies may share a chunking implementation or provide their own chunker when parsing a format requires different boundaries or extracted text.

### 10.2 Boundary priority

Use this priority:

```text
heading section
paragraph
list block
fenced code block
sentence
token window
```

### 10.3 Chunking algorithm

1. Parse the Markdown into logical blocks.
2. Associate each block with its nearest heading path.
3. Tokenize each block using the tokenizer matching the embedding model.
4. Accumulate blocks until the next block would exceed `maxTokens`.
5. Emit the current chunk.
6. Build overlap from the end of the previous chunk.
7. Continue until all blocks are consumed.
8. If a single block exceeds `maxTokens`, split it:
   - by sentence when prose;
   - by line when code;
   - by token window as a final fallback.

### 10.4 Tokenizer accuracy

Character counts and word counts are not sufficient for a strict token limit.

Preferred implementation order:

1. Use the exact tokenizer for the selected embedding model.
2. Use a compatible tokenizer library supported in Go.
3. When exact tokenizer support is unavailable, use an approximate tokenizer and leave a safety margin.

For approximate tokenization:

```text
effective maximum = configured maximum - safety margin
```

Example:

```text
configured maximum: 500
effective maximum: 450
```

This reduces the risk of exceeding the embedding model limit.

The tokenizer implementation must be replaceable:

```go
type Tokenizer interface {
    Count(text string) (int, error)
    Encode(text string) ([]int, error)
    Decode(tokens []int) (string, error)
}
```

### 10.5 Overlap

Default overlap:

```text
50 tokens
```

Overlap should preserve complete sentences or blocks where possible.

Do not duplicate an entire large block merely to achieve exact overlap.

### 10.6 Chunk record

Each generated chunk should include:

```go
type Chunk struct {
    Index       int
    Text        string
    TokenCount  int
    StartOffset int
    EndOffset   int
    HeadingPath []string
    ContentHash string
}
```

---

## 11. LM Studio Embedding Client

Use LM Studio's OpenAI-compatible embeddings endpoint.

Default endpoint:

```text
http://127.0.0.1:1234/v1/embeddings
```

### 11.1 Request

```json
{
  "model": "text-embedding-model",
  "input": [
    "first chunk",
    "second chunk"
  ]
}
```

### 11.2 Response shape

Handle the standard OpenAI-compatible response:

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0123, -0.4567]
    }
  ],
  "model": "text-embedding-model",
  "usage": {
    "prompt_tokens": 123,
    "total_tokens": 123
  }
}
```

### 11.3 Client interface

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}
```

### 11.4 Validation

- Create the Vectorlite table at application startup using the configured embedding dimensions.
- Ensure every returned embedding vector matches the configured dimensions.
- Refuse vectors with incompatible dimensions.

### 11.5 Batching

Default batch size:

```text
16 chunks per HTTP request
```

Make this configurable because different local models and LM Studio configurations have different limits.

### 11.6 Reliability

Implement:

- Request timeout
- Context cancellation
- Exponential backoff
- Retry on transient network errors and HTTP 429/5xx
- No retry on malformed requests or incompatible dimensions
- Clear response-body error reporting
- Maximum retry count

Do not retry indefinitely.

---

## 12. Indexing Workflow

### 12.1 Initial index

```text
open database
load Vectorlite
validate schema
scan root directory
discover Markdown files
for each file:
    read metadata
    determine whether indexing is required
    read content
    hash content
    normalize Markdown
    create chunks
    batch chunks for embedding
    write document, chunks, and vectors transactionally
remove missing files when requested
print summary
```

### 12.2 Per-document replacement

When a document changes:

1. Generate and embed the new chunks before deleting the old data when practical.
2. Begin a database transaction.
3. Delete old vector rows.
4. Delete old chunk rows.
5. Update the document row.
6. Insert new chunk rows.
7. Insert new vectors using the corresponding chunk IDs.
8. Commit.
9. Roll back on failure.

Because Vectorlite virtual-table operations may have transaction-specific behavior, verify atomicity with the selected Vectorlite version.

Document processing is staged with an explicit status:

```text
indexed
scanned
chunked
embedded
done
failed
```

The CLI `index` command orchestrates the internal stages in order. The index stage crawls and stores filesystem metadata, then marks records as `indexed`. The scan stage reads `indexed` records one by one, compares current metadata with the previous scan checkpoint, hashes files when metadata differs, stores changed content hashes, and advances records to `scanned` or `done`. The chunking stage reads `scanned` records one by one, replaces their chunk rows, and advances them to `chunked`. The embedding stage reads `chunked` records one by one, stores chunk embeddings, and advances completed documents to `done`.

Search must only include documents that have completed the required downstream stages.

### 12.3 Missing files

When `--remove-missing` is enabled:

1. Collect all discovered relative paths.
2. Query indexed documents under the same root.
3. Find records absent from the discovered set.
4. Delete their vector rows.
5. Delete their document records.

Foreign-key cascades should remove chunk records.

### 12.4 Full reindex

`--full-reindex` must:

- Remove all indexed documents for the selected root.
- Remove associated chunk vectors.
- Recreate data using the current model and chunking configuration.

Use full reindex when any of these change:

```text
embedding model
embedding dimensions
distance metric
chunking implementation version
maximum tokens
overlap policy
normalization strategy
```

---

## 13. Search Workflow

### 13.1 Query processing

```text
read query
normalize query
embed query using LM Studio
validate vector dimensions
search Vectorlite for top candidate chunks
join chunk hits to documents
group results by document ID
score documents
return top documents with best matching chunks
```

### 13.2 Candidate retrieval

To return 10 documents, do not retrieve only 10 chunks.

Default:

```text
requested documents: 10
candidate chunks:    100
```

Reason:

The best 10 chunk hits may belong to only one or two documents.

Make `candidate-chunks` configurable.

### 13.3 Grouping by document

Group all candidate chunks by `document_id`.

For the initial implementation:

```text
document score = best chunk score
```

When Vectorlite returns distance where lower is better:

```text
document score is based on minimum chunk distance
```

When the output requires a normalized similarity score, convert carefully according to the selected distance metric. Do not invent a universal conversion that is mathematically invalid for the metric.

### 13.4 Result ranking

Rank documents using:

1. Best chunk score
2. Second-best chunk score as an optional tie-breaker
3. Relative path as the final deterministic tie-breaker

Recommended optional combined score:

```text
document_score =
    best_chunk_similarity
    + 0.20 * second_best_similarity
    + 0.10 * third_best_similarity
```

Do not use this combined score until the basic best-chunk strategy has been tested.

### 13.5 Returned chunks

For every returned document, include up to `chunks-per-document` best matching chunks.

Default:

```text
3 chunks per document
```

Keep chunk order metadata so the caller can reconstruct local context.

Optional context expansion:

When the best chunk has index `N`, allow returning adjacent chunks:

```text
N - 1
N
N + 1
```

This should be a separate option because adjacent chunks are not necessarily among the vector hits.

---

## 14. Search SQL Shape

Vectorlite-specific search SQL must remain encapsulated.

The application-level result should look like:

```go
type VectorHit struct {
    ChunkID  int64
    Distance float64
}
```

Then load metadata:

```sql
SELECT
    c.id,
    c.document_id,
    c.chunk_index,
    c.text,
    c.token_count,
    d.absolute_path,
    d.title
FROM chunks c
JOIN documents d ON d.id = c.document_id
WHERE c.id IN (...);
```

Group in Go rather than relying on complex SQL around the virtual table.

Benefits:

- Simpler Vectorlite integration
- Easier score logic
- Easier multi-chunk result construction
- Easier testing
- Easier future migration to another vector engine

---

## 15. Search Output

### 15.1 Human-readable output

```text
1. docs/payments/configuration.md
   Score: 0.9123

   Best match:
   "The payment provider can be configured through..."

   Additional match:
   "For production credentials, configure..."
```

### 15.2 JSON output

```json
{
  "query": "how is payment configuration handled",
  "model": "text-embedding-model",
  "results": [
    {
      "document_id": 42,
      "absolute_path": "/Users/example/docs/payments/configuration.md",
      "score": 0.9123,
      "chunks": [
        {
          "chunk_id": 308,
          "chunk_index": 4,
          "score": 0.9123,
          "text": "The payment provider can be configured through..."
        }
      ]
    }
  ]
}
```

The output must clearly distinguish:

```text
distance
similarity
rank
```

Do not label a raw distance as similarity.

---

## 16. Concurrency

### 16.1 Indexing pipeline

Use a bounded pipeline:

```text
crawler
  → file readers
  → chunkers
  → embedding batches
  → single database writer
```

Recommended initial implementation:

- Concurrent file reading and chunking
- Limited concurrent embedding requests
- Single writer goroutine for SQLite

SQLite generally behaves best when writes are serialized.

### 16.2 Concurrency configuration

Suggested flags:

```text
--workers
--embedding-concurrency
```

Defaults:

```text
file workers:           runtime.NumCPU()
embedding concurrency:  1 or 2
database writers:       1
```

Avoid unbounded goroutines.

---

## 17. Failure Handling

### File-level errors

A malformed or unreadable Markdown file must not abort the entire indexing run unless `--fail-fast` is enabled.

Record and report:

```text
path
stage
error
```

Stages:

```text
discover
read
normalize
chunk
embed
store
delete
```

### Embedding failures

When a batch fails permanently:

- Mark every affected document as failed.
- Do not publish partial chunks for that document.
- Continue with unrelated documents unless fail-fast mode is active.

### Database failures

Database initialization or schema errors are fatal.

A per-document transaction failure should roll back that document and continue when safe.

---

## 18. Logging

Use structured logging.

Recommended standard package:

```text
log/slog
```

Log levels:

```text
DEBUG
INFO
WARN
ERROR
```

Example:

```json
{
  "level": "INFO",
  "event": "document_indexed",
  "path": "docs/payments.md",
  "chunks": 8,
  "duration_ms": 241
}
```

Do not log full document text or full embedding vectors by default.

---

## 19. Security

- Bind LM Studio to localhost unless remote access is explicitly required.
- Validate embedding endpoint URLs.
- Apply HTTP timeouts.
- Never execute content from Markdown files.
- Do not interpret Markdown code blocks.
- Avoid following symlinks by default.
- Prevent directory traversal when resolving relative paths.
- Store database and extension paths explicitly.
- Never dynamically load arbitrary extensions discovered in indexed directories.
- Load only the configured Vectorlite extension.

---

## 20. Performance Guidance

### Indexing

- Batch embedding requests.
- Reuse one HTTP client.
- Reuse one database connection pool.
- Serialize SQLite writes.
- Skip unchanged documents.
- Use prepared statements.
- Use transactions for document replacement.
- Store vectors as `float32`.
- Avoid loading the complete corpus into memory.

### Search

- Retrieve more chunk candidates than the requested document count.
- Join metadata only for returned candidate chunk IDs.
- Group candidates in Go.
- Return only the required chunks and fields.
- Avoid retrieving full documents during search.

---

## 21. Testing Strategy

### Unit tests

Test:

- Markdown discovery
- Hidden-directory behavior
- Path normalization
- SHA-256 hashing
- Markdown block splitting
- Token-limit enforcement
- Chunk overlap
- Oversized sentence splitting
- Oversized code-block splitting
- Document grouping
- Score ordering
- LM Studio response parsing
- Dimension validation

### Integration tests

Use:

- Temporary directories
- Temporary SQLite databases
- A fake HTTP embedding server
- A real Vectorlite extension in platform-specific CI where possible

Integration scenarios:

1. Index one Markdown file.
2. Search and retrieve it.
3. Index multiple files.
4. Return unique documents despite repeated matching chunks.
5. Modify one file and reindex only that file.
6. Delete one file and remove its records.
7. Reject model-dimension mismatch.
8. Recover from an embedding request failure.
9. Handle empty Markdown files.
10. Handle a Markdown file larger than memory-safe thresholds.

### Deterministic fake embedder

For tests, implement a fake embedder:

```go
type FakeEmbedder struct {
    Dimensions int
}
```

It should generate deterministic vectors from input hashes.

Do not require LM Studio for unit tests.

---

## 22. Suggested Go Interfaces

```go
type DocumentCrawler interface {
    Crawl(ctx context.Context, root string) (<-chan FileCandidate, <-chan error)
}

type MarkdownReader interface {
    Read(path string) (DocumentContent, error)
}

type Chunker interface {
    Chunk(ctx context.Context, doc DocumentContent) ([]Chunk, error)
}

type Tokenizer interface {
    Count(text string) (int, error)
    Encode(text string) ([]int, error)
    Decode(tokens []int) (string, error)
}

type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Model() string
}

type Repository interface {
    Initialize(ctx context.Context) error
    GetDocument(ctx context.Context, rootPath, relativePath string) (*Document, error)
    ReplaceDocument(ctx context.Context, doc Document, chunks []Chunk, vectors [][]float32) error
    DeleteDocuments(ctx context.Context, documentIDs []int64) error
    SearchChunks(ctx context.Context, vector []float32, limit int) ([]VectorHit, error)
    LoadChunkMetadata(ctx context.Context, chunkIDs []int64) ([]ChunkMetadata, error)
}
```

---

## 23. Suggested Domain Types

```go
type FileCandidate struct {
    AbsolutePath string
    FileID       string
    Size         int64
    ModifiedAtNS int64
}

type DocumentContent struct {
    FileCandidate
    Title       string
    Text        string
    ContentHash string
}

type Document struct {
    ID           int64
    FileID       string
    AbsolutePath string
    FileSize     int64
    ModifiedAtNS int64
    ContentHash  string
    Title        string
}

type Chunk struct {
    ID           int64
    DocumentID   int64
    Index        int
    Text         string
    TokenCount   int
    StartOffset  int
    EndOffset    int
    HeadingPath  []string
    ContentHash  string
}

type SearchResult struct {
    DocumentID   int64
    AbsolutePath string
    Title        string
    Score        float64
    Chunks       []SearchChunk
}

type SearchChunk struct {
    ChunkID   int64
    ChunkIndex int
    Score      float64
    Text       string
}
```

---

## 24. Database Versioning

Use explicit schema migrations.

Example:

```text
migrations/
  001_initial.sql
  002_add_document_state.sql
  003_add_heading_path.sql
```

Store the active schema version in `index_metadata`.

Do not silently mutate incompatible vector schemas.

A vector dimension change requires creating a new vector table or performing a full reindex.

---

## 25. Recommended Implementation Phases

### Phase 1: Basic indexing

- CLI skeleton
- Recursive Markdown discovery
- SQLite schema
- LM Studio embedding client
- Simple paragraph-based chunking
- Vectorlite inserts
- Full initial index

### Phase 2: Search

- Query embedding
- Vectorlite candidate search
- Chunk metadata loading
- Grouping by document ID
- Human-readable and JSON output

### Phase 3: Incremental indexing

- Modification detection
- Content hashes
- Per-document replacement
- Missing-file deletion
- Full-reindex option

### Phase 4: Better chunking

- Markdown block parsing
- Heading context
- Token-aware overlap
- Oversized block fallback
- Exact model tokenizer integration

### Phase 5: Hardening

- Retries
- Structured logging
- Migration support
- Cross-platform extension loading
- Integration tests
- Performance benchmarks

---

## 26. Acceptance Criteria

The implementation is complete when all of the following are true:

1. The CLI recursively discovers Markdown files under a supplied root directory.
2. Each document is deterministically split into chunks no larger than the configured token limit.
3. Adjacent chunks use the configured overlap policy.
4. Chunks are embedded through LM Studio's embeddings endpoint.
5. Documents and chunks are stored in SQLite.
6. Chunk vectors are stored and searched through Vectorlite.
7. Re-running index skips unchanged files.
8. Changed files replace their previous chunks and vectors.
9. Missing files can be removed from the index.
10. Search embeds the query using the same model used for indexing.
11. Search retrieves more candidate chunks than requested documents.
12. Candidate chunks are grouped by document ID.
13. The same document appears at most once in the final result list.
14. Every result includes its best matching chunk.
15. JSON output is stable and suitable for consumption by another process.
16. Model or vector-dimension mismatches produce explicit errors.
17. Indexing failures do not leave searchable partial documents.
18. Unit tests do not require a running LM Studio instance.
19. Vectorlite-specific SQL is isolated behind a storage abstraction.
20. The application runs as a local CLI without a database server.

---

## 27. Initial Recommended Defaults

```text
file types:             .md, .markdown
max chunk tokens:       500
chunk overlap:          50 tokens
embedding batch size:   16
embedding concurrency:  1
candidate chunks:       100
returned documents:     10
chunks per document:    3
document score:         best chunk score
distance metric:        cosine
database engine:        SQLite
vector extension:       Vectorlite
embedding provider:     LM Studio
```

---

## 28. Important Implementation Notes for Codex

- Verify the exact Vectorlite SQL syntax against the version included in the project before writing migrations.
- Do not assume that generic SQLite vector-extension syntax is valid for Vectorlite.
- Keep Vectorlite queries isolated in one package.
- Do not substitute `sqlite-vec` or another vector extension unless explicitly requested.
- Do not implement a server.
- Do not use an LLM for chunking.
- Do not return duplicate documents in final search results.
- Do not mix embeddings from different models or dimensions.
- Prefer clear, testable packages over a single large command implementation.
- Build the simplest working indexing and search path first, then add incremental indexing and advanced chunking.
