# Contributing to LeaseGrid

Thanks for your interest! LeaseGrid is in early development, so the codebase is small and changes quickly.

## Prerequisites

- Go 1.26 or newer (see the `go` line in `go.mod`)
- Git

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
gofmt -l .        # must print nothing; fix with: gofmt -w .
go vet ./...
go test ./...
go build ./...
```

CI also runs [golangci-lint](https://golangci-lint.run/). Running it locally is optional.

## Guidelines

- Keep pull requests small and focused on one change.
- Add or update tests for any behaviour change. Prefer table-driven tests.
- Use only the standard library unless a dependency clearly pays for itself.
- Write commit messages in the imperative mood: "Add list command", not "Added list command".

## Reporting issues

Open a GitHub issue with what you ran, what you expected, and what happened. Include the output of `leasegrid --version` and `go version`.
