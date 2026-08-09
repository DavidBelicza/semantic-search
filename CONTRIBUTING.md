# Contributing

Bug reports, fixes, and new file formats are welcome. By taking part you agree to the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Build and test

You need Go (the version in [go.mod](go.mod)) and a C compiler, because cgo builds
`mattn/go-sqlite3` and the `sqlite-vec` bindings from source: `xcode-select --install` on
macOS, a C toolchain plus `libsqlite3-dev` on Linux, MSYS2 with mingw-w64 on Windows.

```bash
go build ./...
make test    # go test ./...
make lint    # golangci-lint run ./...
```

## Before you open a pull request

1. `make test` passes with no database running. Nothing in the library may need a server to be
   testable.
2. `make lint` reports no issues.
3. Coverage stays at 100% for every package the library ships. `examples/`, `test/e2e/`, and
   `migrations/` are excluded in [codecov.yml](codecov.yml).

For a bug fix, include a test that fails without the fix. Report security issues privately as
described in [SECURITY.md](SECURITY.md), not in a pull request or issue.

## Code style

**Keep functions flat.** Use early returns, guard clauses, and extracted helpers instead of
deeper indentation.

- `if` inside an `if` is not acceptable. Flatten it.
- `if` inside a loop is fine.
- A loop inside an `if` is not. Invert the condition, return early, then loop at the top level.
- Three nested loops are not acceptable. Extract.

`nestif` in [.golangci.yml](.golangci.yml) enforces the first rule, so `make lint` will tell
you. Two smaller conventions: one test file per production file (`parser.go` and
`parser_test.go`), and comments that explain **why** rather than what.

## Adding a file format

Formats are strategies and touch no other layer. Use
[core/strategy/config](core/strategy/config) as the model:

1. Create `core/strategy/<format>/`, embedding `general.GeneralStrategy` and overriding only
   `Claims`, `Parse`, and `Chunk` where the format needs it.
2. Register a factory in [semanticsearch.go](semanticsearch.go) with the extensions it claims.
3. Update [README.md](README.md), [docs/chunking.md](docs/chunking.md), and
   [docs/todo.md](docs/todo.md).
4. Add a fixture and a query to [test/e2e/e2e_test.go](test/e2e/e2e_test.go).

`Parse` receives only bytes, never the file path. If your format needs the path to pick a
parser, carry the text through `Parse` and do the work in `Chunk`, which has it.

## Notes

- Database error paths are covered with [internal/dbmock](internal/dbmock), a scriptable
  `database/sql` driver that needs no server. Where even that cannot reach, the code uses a
  package-level seam such as `var openDB = sql.Open`. No error is muted to satisfy coverage.
- The dependency list is short on purpose. A new one needs a reason the standard library cannot
  cover, and pure Go is preferred over cgo. Say why in the pull request.
- The Postgres tests skip unless `SEMANTIC_SEARCH_POSTGRES_DSN` points at a database. Start one
  with `docker compose -f test/docker/docker-compose.yml up -d`.
