# LemonTube conventions

## General

- Write idiomatic, maintainable Go rather than mechanically translating another application's implementation. Prefer simple designs and avoid abstractions that do not solve a concrete problem.
- Optimize code for human readers. Use multiline branches, loops, struct/map literals, and chained calls where that improves readability; do not compress control flow onto one line.
- Format Go code with `gofumpt`.
- Commit after each feature or fix using conventional commits. Never mix unrelated changes in one commit; amend or split commits when needed.

## Go organization and validation

- Put the executable entry point in `cmd/lemontube`. Application logic, domain types, jobs, and repository code live directly in `internal`; `internal/server` and `internal/ytdlp` are subpackages. Do not introduce additional packages without a clear need.
- Define named types and constants for enums. Give enum types a `Validate() error` method; validate input at the service boundary.
- The repository should trust validated domain inputs instead of repeating service validation.
- Prefer pointers for nullable values over `sql.Null*` types.

## SQLite and repository

- Use `modernc.org/sqlite`. Encapsulate database operations in the repository; handlers and services should not issue SQL directly.
- Use goqu: construct a query first (`q := r.db.From(...)`, etc.), then execute it. Break long chains and records across lines. For row queries, prefer `Select(&rowType{})` so goqu selects the fields from the row type.
- Use private row structs with `db` tags and `toDomain` methods to map persistence types to domain types. Prefer `r.db.WithTx()` for goqu transactions.
- Store structured values as SQLite `JSON`, not JSONB or generic BLOB columns. Use a generic `JSON[T]` type implementing `sql.Scanner` and `driver.Valuer` to encapsulate serialization.
- Use readable, consistently formatted UTC ISO 8601 timestamps, SQLite `BOOLEAN` columns with appropriate checks, foreign keys, and column defaults where appropriate, including creation timestamps.
- Enforce database integrity in the schema using SQLite constraints and built-in expressions. Keep those checks independent of Go domain validation; do not register Go domain validators as SQLite functions.
- Format SQL for readability and annotate Go SQL strings with `// language=sql` for editor highlighting.

## Tests

- Use Testify (`require` and `assert`) and `t.Context()` where a context is needed.
- Factor repeated test setup into helpers using `t.Helper()` and `t.Cleanup()`.
- Prefer table-driven tests; where cases have different expectations, give each case an `assertResult(t, result)` callback rather than branching over expectation flags.
- Run `go test ./...`; use `go test -race ./...` for concurrency-sensitive changes.

## Frontend

- Use Alpine.js for frontend behavior, with JSDoc annotations on JavaScript function signatures.
- Keep `web/fs.go` responsible for returning embedded assets by default and files from disk when `DEBUG=1`.
- Use clear, direct, end-user-focused copy. This is a personal tool, not a marketing site; avoid decorative or playful text that does not help the user.
- Use consistent sizing, readable contrast, accessible focus and keyboard interactions, and layouts that work on mobile.
- Use modern CSS features where useful, including customizable selects, anchor positioning, starting styles, and transitions. Provide usable fallbacks for browsers without support and respect reduced-motion preferences.
- File selection should support accessible click/keyboard interaction and drag-and-drop where appropriate; cookie input also supports pasting a file or its text into an editable textarea.

## Dependencies and safety

- Use uv for Python package management in the Docker image.
- Preserve the lazy-cleanup safety model: isolate job workspaces and only remove files LemonTube can establish it owns. Never sweep arbitrary existing downloads.
