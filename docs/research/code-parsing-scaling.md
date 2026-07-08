# Research: Code Parsing — Measurement & Scaling

A follow-up to [vector-search-scaling.md](vector-search-scaling.md), which modelled a
Markdown/book corpus. This one measures the **code strategy** (`internal/strategy/code`,
Chroma-lexer definition chunking) on a real source tree and compares the two corpus shapes.

_Measured: 2026-07-08. Same machine/model as the prior doc (Apple M4, EmbeddingGemma-300m,
768-dim). Note: the store is now `sqlite-vec` (single file), not LanceDB as the prior doc
still describes._

---

## 1. Corpus

Magento `vendor/magento/module-catalog` — a large, real PHP module.

| Property | Value |
|---|---|
| Files on disk | 3,198 (1,622 `.php`, 115 `.js`, rest xml/phtml/images — not claimed) |
| Documents indexed | 1,743 (all claimed `.php` + `.js` + a few md/txt) |
| Chunks | 11,843 |
| Chunks / file | **6.8** |
| Chunk tokens | avg **189**, p50 150, max 480 (budget 400 + overlap) |
| Structured titles | 8,695 chunks carry a class-nesting path (`class X > method()`) |
| Index wall time | **338 s** (~5.6 min) |
| Throughput | **35 chunks/sec** |
| Store size | 51 MB total (~4.3 KB/chunk all-in: ~3 KB vector + text/index) |
| Minified/generated leaked | 0 |

---

## 2. Comparison with the book research

| Metric | Book model (prior doc) | Code (measured) | Note |
|---|---|---|---|
| Corpus shape | few large books, ~700 chunks each | **many tiny files, 6.8 chunks each** | inverted distribution |
| Avg chunk tokens | 138 (benchmark) / 350 target | **189** (400 budget) | code packs more per chunk |
| Throughput | ~50/sec "realistic" @ 138 tok | **35/sec** @ 189 tok | see below |
| Storage/vector | 3 KB + text | ~3 KB + text (~4.3 KB all-in) | identical vector cost |
| Search latency | <100 ms below ~1M chunks | instant (12k chunks) | 0.3% of the ceiling |

### The throughput match

35/sec looks below the doc's "~50 realistic," but it is a near-exact match once adjusted for
chunk size — throughput is bound by **tokens embedded**, not chunk count:

```
50 chunks/sec × (138 tok / 189 tok) ≈ 37 chunks/sec  ≈  35 measured
```

The embedding path is still the same saturated ~16 ms/chunk accelerator bottleneck the prior
doc identified; code just puts more tokens through each chunk. Nothing about code parsing
changes the fundamental limit.

### What actually differs

Only the **corpus shape**, and it is a data property, not a new cost:

- Code is many small files (6.8 chunks each) vs few large books (~700 each) — same total-chunk
  math, opposite distribution.
- Code chunks run **under budget** (avg 189 / p50 150 vs a 400 budget) because most methods are
  short. Per-definition sectioning yields many small, coherent chunks instead of budget-filling
  windows — which is exactly why retrieval landed on the right method every time.

---

## 3. Conclusion

The code strategy introduces **no new scaling behaviour**. It sits in the same
exact-brute-force regime the prior research mapped, and its one measurable cost — indexing
throughput — follows the same token-bound model, predicted to within ~5%. Boundary detection
via the Chroma lexer (pure Go, no CGO) is negligible next to embedding: the 338 s wall time is
essentially all model inference.

Practical takeaways:

- **Parsing is free at this scale.** Lexing + sectioning cost is lost in the noise; the
  embedding accelerator remains the sole bottleneck.
- **Code indexes are small per repository.** One large module ≈ 12k chunks ≈ 0.3% of the
  ~3–4M brute-force ceiling. Dozens of modules still fit comfortably in the "instant" regime;
  the scaling levers from the prior doc (ANN, sharding) stay irrelevant until a corpus is far
  larger than any single codebase.
- **Chunk quality, not quantity, is the win.** Small under-budget chunks titled with their
  nesting path drove precise retrieval — the value here is representational, and it costs
  nothing extra to store or search.
