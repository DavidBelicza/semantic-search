<p align="center">
  <img src=".github/banner.jpeg" alt="Semantic Search: meaning over keywords" width="620">
</p>

<h1 align="center">Semantic Search</h1>

<p align="center">A semantic search tool that <strong>recursively indexes</strong> PDF, Markdown, code, and many other file types in a directory, then <strong>chunks</strong> them and stores them in a <strong>vector database</strong> using an <strong>embedding AI model</strong>. It enables <strong>meaning-based search</strong> across your documents, working as a perfect backend, like a <strong>clone of Google NotebookLM's search engine</strong>.</p>

## Supported formats

| Format | Extensions | How it's chunked |
|---|---|---|
| Markdown | `.md`, `.markdown`, `.mdown` | Split by headings, with code blocks kept whole |
| PDF | `.pdf` | Headings detected from font sizes, read in natural page order |
| Plain text | `.txt`, `.text`, `.log`, `.rst`, `.org`, `.adoc` | Split into overlapping paragraphs |
| Code | `.go`, `.js`, `.ts`, `.jsx`, `.tsx`, `.py`, `.php`, `.java`, `.rb`, `.rs`, `.c`, `.h`, `.cpp`, `.hpp`, `.cs`, `.sh`, `.sql` | One section per function or class, titled with its full path |
| DOCX | `.docx` | Split by Word heading styles |

## How it works

1. **Index**: walk the tree; the strategy pool picks the strategy that claims each file.
2. **Parse**: decode bytes into heading/definition-structured sections.
3. **Chunk**: pack sections into token-budget chunks with overlap, each carrying its title path.
4. **Embed**: turn chunks into vectors via the embedding server.
5. **Search**: embed the query and rank chunks by vector distance using exact k-nearest-neighbor (kNN) search, comparing against every chunk for precise results.

## Requirements

- **Go**, and a **C compiler** so cgo can build `mattn/go-sqlite3` and the `sqlite-vec`
  bindings from source. Install the compiler for your system:
  - **macOS**: `xcode-select --install` (Clang)
  - **Debian / Ubuntu**: `sudo apt install build-essential`
  - **Fedora / RHEL**: `sudo dnf install gcc`
  - **Windows**: install a MinGW-w64 gcc toolchain (e.g. via MSYS2) and add it to `PATH`
  - **Windows (alternative)**: use [WSL2](https://learn.microsoft.com/windows/wsl/install) and
    follow the Debian / Ubuntu steps inside your Linux distribution
- An **OpenAI-compatible embedding server** on `http://127.0.0.1:1234` (e.g. LM Studio)
  serving an embedding model such as EmbeddingGemma-300m (768-dim).

## Build, test, lint

```sh
go build ./...   # build (cgo)
make test        # go test ./...
make lint        # golangci-lint
```

## Usage

Index a directory:

```sh
semantic-search index ./path/to/files
```

Search (arguments: result limit, then query):

```sh
semantic-search search 5 "how do I detect security threats in logs"
```

Common flags:

- `--db <path>`: SQLite database path (default `vector-index.db`).
- `--include-hidden`: index hidden files and directories.
- `--follow-symlinks`: follow symbolic links.
- `--fail-fast`: abort on the first document error instead of continuing.

Re-running `index` is incremental: unchanged files (by content hash) are not re-embedded.

## Documents

### Reference

- [docs/architecture.md](docs/architecture.md): how the pipeline and strategies fit together.
- [docs/chunking.md](docs/chunking.md): how each format is parsed and chunked.

### Research

- [docs/research/vector-search-scaling.md](docs/research/vector-search-scaling.md): indexing and search performance, limits, and scaling options.
- [docs/research/code-parsing-scaling.md](docs/research/code-parsing-scaling.md): measurements for code parsing, compared with the book corpus.
- [docs/research/sqlite-vec-migration.md](docs/research/sqlite-vec-migration.md): moving the vector store to sqlite-vec.
- [docs/research/pdf-extraction-engine.md](docs/research/pdf-extraction-engine.md): PDF text extraction with PDFium.

## License

Released under the [MIT License](LICENSE).
