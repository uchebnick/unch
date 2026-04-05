# Contributing

## Local Setup

`unch` is a Go project. The normal local loop is:

```bash
go test ./...
go build -o unch ./cmd/unch
```

For an end-to-end smoke test against the current checkout:

```bash
go run ./cmd/unch index --root .
go run ./cmd/unch search --root . "command dispatch"
```

First run may download the default embedding model, fetch local `yzma` runtime libraries, and create `./.semsearch/`.

## Before Opening a Change

- keep the CLI surface honest in docs and examples
- prefer repo-relative behavior over machine-specific paths
- run `go test ./...`

If you touch the generated searcher workflow or its template, make sure the checked-in workflow and the generated scaffold still match current behavior.

## Reporting Issues

- bugs: include repo type, language, query, and the unexpected result
- search quality reports: include the query and the result you expected to rank higher
- CI or remote issues: include the workflow URL or failing run URL when possible
