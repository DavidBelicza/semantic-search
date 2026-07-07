# PDF text-extraction engine

Why the PDF strategy uses PDFium (via `go-pdfium`, WebAssembly build) rather than a
pure-Go reader. Background notes, not kept in sync with the code.

## Constraints

- Offline, self-contained, no separate binary to install (matches the sqlite-vec model).
- Permissive license, so the engine can be embedded in a closed-source, distributable app.
- No OCR: scanned/image-only PDFs are out of scope; only the text layer is extracted.

## Options considered

| Engine | License | Distribution | Quality |
|---|---|---|---|
| `ledongthuc/pdf` (pure Go) | MIT | none needed | weak, fragile on real files |
| PDFium via `go-pdfium` (WASM) | BSD | embedded in binary | strong |
| MuPDF via `go-fitz` | AGPL / commercial | bundled native libs | strong |
| UniPDF | commercial | native | strong |
| Poppler `pdftotext` | GPL | user installs binary | strong |

PDFium is the only option that is simultaneously actively maintained, high quality,
permissively licensed, offline, and self-contained. Its WASM build embeds the engine in the
binary and needs no CGO or installed library.

## Measured comparison

A 58-file PDF corpus, `ledongthuc/pdf` vs `go-pdfium` (WASM):

| Metric | ledongthuc | go-pdfium (WASM) |
|---|---|---|
| Files with text extracted | 41 | 56 |
| Empty results | 5 (3 false negatives) | 2 (genuine image scans) |
| Parser errors | 12 | 0 |
| Total characters | 134,642 | 172,055 |
| Per-file extraction | ~2.8 ms | ~6.4 ms |
| One-time init (wazero compile) | ~0 | ~1.1 s |
| Binary size | 14.6 MB | 24.7 MB |

`ledongthuc` failed on two mainstream cases a stronger engine handles: a space before the
newline in the `%PDF-1.3` header (12 files, strict header check) and cross-reference /
object streams introduced in PDF 1.5 (3 files). Only 2 files are genuinely image-only.

## Cost of the WASM build

Same PDFium engine as the native CGO build, so extraction output is identical. The trade-offs
are runtime-only:

- **Binary +~10 MB** — the embedded `pdfium.wasm` (~5 MB) plus the wazero runtime.
- **~1.1 s one-time init** — wazero compiles the module per process. Paid once at startup;
  negligible next to embedding-model calls.
- **~2× slower per file** — irrelevant for background batch indexing of small documents.

If a performance-sensitive build ever needs it, switching to the native CGO build is a
one-file change behind the `PDFTextExtractor` interface, at the cost of shipping a native
library. For now the self-contained WASM build wins.
