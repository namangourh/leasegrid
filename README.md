# LeaseGrid

[![CI](https://github.com/namangourh/leasegrid/actions/workflows/ci.yml/badge.svg)](https://github.com/namangourh/leasegrid/actions/workflows/ci.yml)

LeaseGrid is a Go CLI for viewing compute leases across DePIN providers.

## Status

Early development. The current release proves the CLI foundation only.

## Install

Requires Go 1.26 or newer.

```sh
go install github.com/namangourh/leasegrid/cmd/leasegrid@latest
leasegrid --version
```

## Run

```sh
go run ./cmd/leasegrid --help
go run ./cmd/leasegrid --version
```

Expected version output:

```text
leasegrid 0.1.0
```

## Test and build

```sh
go test ./...
go build ./...
```

## Roadmap

1. CLI foundation
2. Normalized lease model and provider interface
3. Mock provider implementation
4. Akash provider integration

## Learn more

- [DESIGN.md](DESIGN.md): technical design and package boundaries
- [CONTRIBUTING.md](CONTRIBUTING.md): development setup and checks
- [LICENSE](LICENSE): MIT
