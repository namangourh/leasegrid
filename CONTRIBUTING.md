# Contributing to LeaseGrid

Thanks for your interest! LeaseGrid is in early development, so the codebase is
small and changes quickly.

## Prerequisites

- Go 1.26 or newer (see the `go` line in `go.mod`)
- Git
- For `go test -race`: cgo enabled and a C compiler (on Windows, for example, gcc
  from MSYS2 or TDM-GCC)

## Getting started

```sh
git clone https://github.com/namangourh/leasegrid.git
cd leasegrid
go test ./...
go run ./cmd/leasegrid --help
```

## Before opening a pull request

Run the same checks CI runs:

```sh
go mod tidy -diff   # must print nothing
gofmt -l .          # must print nothing; fix with: gofmt -w .
go vet ./...
go test -race ./...
go build ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run
```

The last command downloads and runs the same golangci-lint version CI uses, with
no separate install.

## Guidelines

- Keep pull requests small and focused on one change.
- Add or update tests for any behaviour change. Prefer table-driven tests, and
  write the test first when adding behaviour.
- Use only the standard library unless a dependency clearly pays for itself.
- Return errors; don't call `os.Exit` or `panic` outside `main`.
- Write commit messages in the imperative mood: "Add list command", not
  "Added list command".
- Files use LF line endings (enforced by `.gitattributes`). On Windows, set your
  editor to save with LF.

## Reporting issues

Open a GitHub issue with what you ran, what you expected, and what happened.
Include the output of `leasegrid --version` and `go version`.
