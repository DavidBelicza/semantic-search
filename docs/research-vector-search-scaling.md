# Research: Vector Search & Indexing — Scaling Analysis

A grounded look at how the current local semantic-search solution performs, where
its limits are, and what realistic options exist to scale it. Numbers marked
"measured" were benchmarked on the development machine; others are calculations or
order-of-magnitude estimates with their assumptions stated.

_Last updated: 2026-06-30._

---

## 1. Goal

Local, single-machine semantic search over Markdown notes (and potentially larger
corpora such as books), with no database server and no cloud dependency. Two
distinct performance concerns, which scale independently:

- **Indexing** — converting text chunks into vectors and persisting them. One-time
  (or incremental) cost, bound by the embedding model.
- **Search** — embedding a query and finding nearest chunk vectors. Per-query cost,
  bound by the vector search algorithm.

---

## 2. Hardware & stack (measured)

| Component | Value |
|---|---|
| Machine | Apple **M4**, 24 GB unified RAM, 10 cores (4 performance + 6 efficiency) |
| Embedding server | LM Studio (llama.cpp backend) on `127.0.0.1:1234`, OpenAI-compatible API |
| Embedding model | `text-embedding-embeddinggemma-300m-qat` (≈300M params, quantization-aware) |
| Embedding dimensions | **768** |
| Vector precision | **float32** (4 bytes/value) |
| Vector store | LanceDB (on-disk, columnar) |
| Metadata store | SQLite (source of truth) |
| Search metric | L2 distance on **unit-normalized** vectors → equivalent to cosine |

---

## 3. Data model & sizing math

### Vector size
```
768 dimensions × 4 bytes (float32) = 3,072 bytes ≈ 3 KB per vector
```

### Chunk size
The hard-limit chunker cuts at `maxTokens × avgTokenLength = 300 × 4 = 1,200`
characters per chunk, with **no overlap** currently.

### Chunks per book (500-page novel)
Assumptions: ~250–300 words/page, ~6 chars/word (incl. spaces).

```
500 pages × ~275 words      ≈ 137,500 words
137,500 words × ~6 chars    ≈ 825,000 characters
825,000 / 1,200 chars/chunk ≈ ~690 chunks
```
→ **~700 chunks per 500-page book** (working figure). Real books vary widely:
Harry Potter book 1 (~77K words) ≈ ~400 chunks; book 5 (~257K words) ≈ ~1,300 chunks.

### Storage per book
```
~700 chunks × 3 KB/vector ≈ ~2 MB of vectors (+ ~0.8 MB raw text in SQLite)
```

---

## 4. Embedding throughput (measured)

Benchmarked against LM Studio with the production model and document prefix, ~110-token
chunks:

| Mode | Throughput | Per 60 s |
|---|---|---|
| Sequential, 1 input/request (≈ current pipeline) | **62.5 chunks/sec** | ~3,750 |
| Batched 16/request | 68.7 chunks/sec | ~4,120 |
| Batched 64/request | 68.7 chunks/sec | ~4,120 |
| Concurrency 2 (parallel requests) | 69.2 chunks/sec | ~4,150 |
| Concurrency 4 | 69.4 chunks/sec | ~4,160 |

**Key finding:** batching (+~10%) and parallelism (flat past 2) barely help. The LM
Studio / llama.cpp embedding path is **saturated at ~70 chunks/sec ≈ ~16 ms/chunk**.
Neither sharding the database nor issuing concurrent requests raises this on a single
machine — the bottleneck is the embedding accelerator path, not the pipeline.

Caveat: real chunks average ~138 tokens (up to 300), longer than the test input, so
end-to-end throughput on a full corpus is somewhat lower (~50 chunks/sec realistic
after SQLite/LanceDB writes).

---

## 5. Search: algorithm & cost

### Current behaviour: brute-force (exact) kNN
No ANN index is ever created, so `VectorSearch` performs an **exhaustive scan**: it
computes the distance from the query to **every** vector, then returns the top-k.

- Cost per query ≈ **O(N × D)** — N vectors × 768 dims — and reads the whole vector
  column.
- **Exact** (100% recall). **Touches the entire database on every query.**

### Throughput (order-of-magnitude)
Per vector: ~3 KB to read, ~1,500 math ops to score.

| Data location | Limiter | Vectors scanned / sec |
|---|---|---|
| In RAM (warm) | memory bandwidth / SIMD | ~10–40 million |
| On SSD (cold) | disk read (~2–3 GB/s ÷ 3 KB) | ~1 million |

For interactive latency (~100 ms target), warm RAM covers ~1–3M vectors per query.

### L2 vs cosine
The model returns **unit-length** vectors (verified: stored norms = 1.0000). For unit
vectors, ranking by L2 distance is identical to ranking by cosine similarity, so the
reported `score` (raw L2 distance, lower = closer) behaves like cosine. Normalization
is applied in the store defensively for models that don't normalize.

---

## 6. Capacity on this machine (current brute-force solution)

Binding constraints, in order: **RAM** (vectors should fit for fast search) → **search
latency** → **one-time indexing time**. Using ~700 chunks/book, 3 KB/vector,
~60 chunks/sec.

| Scale | Books | Vectors in RAM | One-time indexing | Search |
|---|---|---|---|---|
| ~700K chunks | ~1,000 | ~2 GB | ~3 h | instant (<100 ms) |
| ~1M chunks | ~1,400 | ~3 GB | ~4.5 h | snappy (~100 ms) |
| ~3.5M chunks | ~5,000 | ~10 GB | ~16 h | sluggish (~0.3–1 s) |
| ~7M+ chunks | ~10,000+ | ~20 GB+ | ~1+ day | multi-second / disk-bound |

**RAM is the real ceiling:** 24 GB is shared with LM Studio's resident models, leaving
~10–14 GB for the vector cache → practical brute-force limit ≈ **3–4 million chunks
≈ ~5,000 books**. Beyond that, search goes disk-bound and slow.

**Realistic verdict (current solution, brute force):**
- Comfortable / snappy: **~1,000–1,500 books**.
- Usable with ~1 s queries and an overnight index: **up to ~5,000 books**.
- Beyond that: needs an ANN index.

---

## 7. The indexing wall

Indexing is the dominant practical limit and is **not** helped by sharding or
concurrency on one machine (see §4).

| Corpus | Books | One-time indexing @ ~60 chunks/sec |
|---|---|---|
| 700K chunks | ~1,000 | ~3 hours |
| 1M chunks | ~1,400 | ~4.5 hours |
| 3.5M chunks | ~5,000 | ~16 hours |
| 10M chunks | ~14,000 | ~46 hours |

### "Index within 1 minute" → how many books?
```
~70 chunks/sec × 60 s = ~4,150 chunks ÷ ~700 chunks/book ≈ ~6 books / minute
```
This is the hard ceiling on this M4. To index **1,000 books in 1 minute** would need
~700K chunks/min ≈ ~11,700 embeds/sec ≈ **~170× this machine's throughput** — i.e. a
fleet of accelerators or a hosted batch embedding service. It is a *compute* problem,
not a software/architecture problem.

---

## 8. Scaling options

### 8.1 ANN index (search-side)
The LanceDB SDK exposes `CreateIndex` with `IVF_PQ`, `IVF_FLAT`, `HNSW_PQ`, `HNSW_SQ`
(currently unused). An ANN index trades exact recall for sub-linear search.

- **IVF_PQ** (recommended at scale): clusters vectors (IVF) and compresses them
  (product quantization). A query probes only the nearest few clusters → scans a
  fraction of N. PQ also compresses ~8–16×.
- **Effect on capacity:** at ~8× compression, 3 KB → ~400 bytes/vector, so 24 GB could
  hold ~60M compressed vectors ≈ **~85,000 books** in RAM, searched in sub-second time.
- **Costs added** (all tiny next to embedding):
  - one-time **training** (k-means centroids + PQ codebooks): seconds–minutes;
  - slightly heavier **inserts** (assign to centroid + encode): sub-ms;
  - periodic **retraining** as the data distribution drifts (re-cluster + re-bucket);
    the vectors are unchanged, only the index's organization is rebuilt.
- **SDK caveat:** v0.1.2 `CreateIndex` exposes only an index *type* (no `nlist` /
  `nprobe` tuning), and `DistanceType` is a no-op stub. Validate tunability before
  relying on it for large-scale recall/latency targets.

**When to adopt:** not before brute force actually hurts (multi-million chunks). Below
that it's complexity (training, retraining, approximate recall) for no perceptible gain.

### 8.2 Sharding + routing (capacity / control / relevance)
Split into multiple independent databases (each its own SQLite + LanceDB) and search
only the relevant ones.

- Keep each shard small (≤ ~1M chunks ≈ ~1,400 books) → **brute force stays fast per
  shard, possibly avoiding ANN entirely.**
- **User-selected search:** the user picks the 2–3 relevant shards; only those are
  scanned (~200–300 ms total, exact).
- **Parent / "meta" routing DB** (a two-level / hierarchical index — conceptually IVF
  at the shard granularity): store per-shard **category/tags** and one or more
  **centroid vectors** (mean embedding, or a few cluster centroids per shard). Query
  flow: embed query → compare to shard centroids/tags → pick top-k shards → search
  those. Trade-off: routing can miss a relevant shard (recall risk, like IVF `nprobe`);
  mitigate with multiple centroids per shard, always including a couple extra shards,
  or category pre-filtering.
- 100 shards × ~1,400 books = **~140,000 books** total capacity, with any query
  scanning only a few shards.
- **Caveat:** sharding does **not** speed up indexing on one machine — total embedding
  work is unchanged; it only lets you *distribute* it across separate hardware.

### 8.3 Faster embedding (indexing-side) — the only local lever
Architecture (sharding/concurrency) cannot move indexing speed, but the embedding
stack can:

- **Better runtime.** Batch-64 ≈ batch-1 strongly implies LM Studio/llama.cpp is **not**
  doing true batched GPU inference — the M4 GPU likely has headroom the current path
  isn't using. A batching-capable Apple-Silicon runtime (**MLX**, or **ONNX Runtime +
  CoreML**) could be materially faster on the same model and hardware. Worth
  benchmarking — the measured "saturation" may be a software limit, not silicon.
- **Smaller model.** EmbeddingGemma-300m is mid-sized; a MiniLM-class model (~22M,
  384-dim) embeds several times faster at some quality cost. The model size is
  effectively the indexing-speed dial.
- **Confirm GPU/Metal usage** in LM Studio (CPU fallback would be much slower).
- Already optimized: content-hash skipping means unchanged files are never re-embedded.

---

## 9. Relevance findings (model + prompt templates)

Initial searches returned irrelevant, tiny results. Investigation (against the real
index) found:

- **Not the metric** — stored vectors already have norm = 1.0, so L2 was already
  cosine-equivalent.
- **Not the chunks** — avg 550 chars / 138 tokens; only 11 of 103 were very short.
- **Root causes:**
  1. The default model id `text-embedding-model` was a placeholder LM Studio rejects
     (HTTP 400). Switched to a real id.
  2. Both available models require **task prompt templates**; omitting them lets junk
     outrank relevant chunks.

Controlled experiment, query "data" vs a relevant vs a junk chunk:

| Setup | cos(query, relevant) | cos(query, junk) | relevant wins |
|---|---|---|---|
| EmbeddingGemma, no prefix | 0.4892 | 0.5143 | ❌ |
| EmbeddingGemma, prompts | 0.4277 | 0.3699 | ✅ |
| nomic, no prefix | 0.5731 | 0.5189 | ✅ (weak) |
| nomic, prompts | 0.6278 | 0.5433 | ✅ (strong) |

**Fix:** prepend prompt templates — documents and queries use different ones — applied
in the embedder (`Prefix`). For EmbeddingGemma: `title: none | text:` (documents),
`task: search result | query:` (queries). For nomic: `search_document:` /
`search_query:`. After re-embedding, "payment" and "data" return the relevant chunks.

**Follow-up:** model id, base URL, dimensions, and prompts are hardcoded; they should
become flags (see `improvements.md` E6). Changing the model or prompts requires a full
re-embed.

---

## 10. Comparison: Google Vertex AI Search

| Aspect | This solution | Vertex AI (Vector) Search |
|---|---|---|
| ANN | none (brute force) | **ScaNN** (anisotropic quantization), distributed |
| Scale | thousands–millions of vectors, 1 machine | **billions** of vectors, large fleet |
| Latency | <100 ms (small) → seconds (millions) | single-digit–tens of ms at billions |
| Embedding dim | **768** | **768** (`text-embedding-004/005`); `gemini-embedding` up to 3072 |
| Embedding model size | ~300M | comparable or larger |

**Takeaway:** the *representation* here (768-dim, ~300M model) is essentially the same
recipe Google uses in production — **not overkill**. The difference is purely
infrastructure scale (one M4 vs a fleet) and indexing throughput (one local model vs a
massive embedding service), not the vector size or model choice. 768 could even be
trimmed (EmbeddingGemma is Matryoshka — truncate 768 → 256) to save storage/compute at
personal scale.

---

## 11. Limits & reasonable solutions (summary)

| Limit | Cause | Reasonable solution |
|---|---|---|
| Indexing ~6 books/min | Embedding model saturates the accelerator path | Faster runtime (MLX / ONNX-CoreML), smaller model, or distribute across machines/hosted API |
| Brute-force search slows past ~1–3M chunks | O(N) scan of the whole table | Add IVF_PQ/HNSW index (sub-linear + PQ compression) |
| RAM ceiling ~3–4M chunks (shared with LM Studio) | float32 vectors must fit for fast search | PQ compression (~8–16×), or shard and search a subset |
| Searching everything is wasteful / unfocused | Single flat index | Sharding + user-selected shards + meta routing DB |
| Relevance | Missing prompt templates / wrong model id | Correct model id + document/query prompts (done); make configurable |
| No tuning of metric/index params | SDK v0.1.2 stubs `DistanceType`, limited `CreateIndex` | Validate or upgrade the SDK before relying on ANN at scale |

**One-line conclusion:** the current solution is a correct, single-machine version of a
standard production retrieval recipe — excellent to ~1,000–1,500 books with exact
brute-force search. Scaling *search* is a solved problem (ANN and/or sharding+routing);
scaling *indexing* on a single machine is fundamentally a compute problem and the real
ceiling.
