# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Project references

- Design spec: [docs/go-vector-indexer-implementation.md](docs/go-vector-indexer-implementation.md)
- Improvements backlog (the work to do, with a "Deferred — not now" section):
  [docs/improvements.md](docs/improvements.md)
- Chunking design (implemented): [docs/chunking-design.md](docs/chunking-design.md)
- Scaling/throughput research: [docs/research-vector-search-scaling.md](docs/research-vector-search-scaling.md)
- sqlite-vec migration (implemented; replaced LanceDB): [docs/research-sqlite-vec-migration.md](docs/research-sqlite-vec-migration.md)

## Code style: prefer a flat structure

Avoid nesting blocks inside a single function. Reach for early returns / guard
clauses and extract helper functions instead of deepening indentation.

Rules:

- **`if` inside an `if` — not acceptable.** Flatten with a guard clause, an early
  `return` / `continue`, or by extracting a helper function.
- **`if` inside a loop — acceptable.**
- **Loop inside an `if` / condition — not acceptable.** Invert the condition and
  early-`return` / `continue`, then run the loop at the top level.
- **Loop inside a loop (2 levels) — questionable.** Prefer extracting the inner loop
  into a named function; keep it nested only when that is clearly the simplest
  correct form.
- **Loop nested 3 levels deep — not acceptable.** Always extract.

Enforcement:

- `make lint` runs golangci-lint. **`nestif`** flags deeply nested if-blocks (the
  strict "no `if` inside `if`" rule); the threshold is tunable in
  [.golangci.yml](.golangci.yml). `gocognit` / `gocyclo` back it with overall
  complexity limits, which penalize nesting (including loops in conditions and deep
  loops) but do not encode these as exact rules.
- The loop rules above (loop-in-condition, 2- vs 3-level loops) are largely review
  judgment — golangci-lint has no precise nested-loop check, so `gocognit` is only a
  soft backstop.

## Build, test, lint

- Build: `go build ./...` (cgo; needs a C compiler — `mattn/go-sqlite3` and the
  `sqlite-vec` bindings compile from source)
- Test: `make test` (`go test ./...`)
- Lint: `make lint` (requires the `golangci-lint` binary)

## Skills

Project skills live in `.claude/skills/`. Available: `git-commit-and-push`.
