# Security Policy

Semantic Search is a Go library for meaning-based search over local documents. It reads files
from disk, embeds them through an OpenAI-compatible model server, and stores the result in
SQLite or PostgreSQL.

## Reporting a vulnerability

Report security issues privately through GitHub:

**[Report a vulnerability](https://github.com/DavidBelicza/semantic-search/security/advisories/new)**

Please do not open a public issue for a security report. Include the version you tested, the
affected package, a short reproduction, and the impact an attacker could achieve. You can
expect an initial response within a few days. Reporters are credited in the published advisory
unless they prefer otherwise. There is no bug bounty program.

## Supported versions

Security fixes are released on the current 1.x minor version. Earlier minors receive no
backports, since the 1.x line is API-compatible and upgrading is a routine dependency bump.

| Version            | Supported          |
| ------------------ | ------------------ |
| Current 1.x minor  | Yes                |
| Earlier 1.x minors | Upgrade to current |

## Security design

Parsing files you did not write is the main risk in a library like this, so that work is
either sandboxed or done in memory-safe code.

- **PDF parsing is sandboxed.** PDFium, the engine behind Chrome's PDF viewer, is compiled to
  WebAssembly and run through the pure-Go [wazero](https://wazero.io) runtime. A malformed or
  malicious PDF that would corrupt memory in a native library stays confined to a WASM
  instance with no filesystem access. There is no CGO in this path and no system library to
  keep patched.
- **Every other parser is pure Go**: goldmark for Markdown, `golang.org/x/net/html` for HTML,
  Chroma for code, and the standard library for DOCX.
- **Parsers extract text only.** Nothing executes embedded code, resolves external entities,
  or fetches remote resources. The HTML strategy drops scripts and styles before reading
  content.
- **There is no server.** The library opens no listening socket and has no authentication
  surface of its own. Its only outbound request is the embeddings call to the endpoint you
  configure.
- **SQL is parameterized** across all four stores. Only compile-time constants and the integer
  embedding dimension are ever interpolated into a query string.
- **Symlinks are not followed by default**, so a symlink planted inside an indexed directory
  cannot pull in files from outside it.
- **Credentials in config files are redacted.** Config formats are the most likely place in a
  corpus to hold secrets, so the config strategy replaces values under keys that name one
  (`password`, `api_key`, `client_secret` and similar) with `[redacted]` before they are
  embedded or stored. The key stays searchable; the secret does not travel.

## Operational notes

- Document text is sent to the embedder at `BaseURL`, which can be a local model server or a
  remote API. Both are supported; the difference is where your content ends up. A local
  endpoint keeps it on the machine, and a remote one sends the text of everything you index to
  that provider. Use HTTPS for any endpoint that is not loopback, since the request carries
  both your content and your `Authorization` header.
- The database stores full chunk text alongside the vectors. Protect it the way you would
  protect the documents it indexed.
- Files are read into memory whole, with no built-in size cap. When indexing untrusted input,
  filter oversized files upstream.
- Load `AiEmbedderConfig.APIKey` from the environment or a secret store rather than source.
- Search results are text from your indexed documents. If you feed them into an LLM prompt,
  treat indirect prompt injection as part of your threat model.

## What is not a vulnerability

- **`fmt.Sprintf` in the vector store SQL.** Only constants and the integer dimension are
  interpolated, and every value is bound through a placeholder.
- **The CGO build requirement.** A C toolchain is needed to build the SQLite bindings. That is
  a build dependency, not a runtime exposure, and the PDF path uses no CGO.
- **Memory and time spent indexing a large corpus you chose to index.**

## Scope

This policy covers the library itself: `core/`, `internal/`, `migrations/`, and the package
root. It does not cover the programs under `examples/`, which are illustrative, or the model
server you point the embedder at.
