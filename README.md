# LeaseGrid

[![CI](https://github.com/namangourh/leasegrid/actions/workflows/ci.yml/badge.svg)](https://github.com/namangourh/leasegrid/actions/workflows/ci.yml)

LeaseGrid is a Go CLI for viewing compute leases across DePIN providers.

## Why

People who deploy on decentralized compute networks such as Akash and Flux need a
different tool for each network. Nothing gives them one view of all their active
leases, what they cost, or when they run out. LeaseGrid aims to be that single view.

## Status

Early development. **No provider is implemented yet. Akash integration is next.**

What works today:

- `leasegrid --help` and `leasegrid --version`
- A YAML configuration file (see [Configuration](#configuration)): loaded, strictly
  parsed and validated
- `leasegrid list`, fully wired from configuration through providers to JSON output,
  and tested against in-memory fake providers. With no provider enabled, it reports
  `no providers configured`; enabling a provider reports that it is not implemented
  yet. Both exit with status 1.


## Install

Requires Go 1.26 or newer.

```sh
go install github.com/namangourh/leasegrid/cmd/leasegrid@latest
leasegrid --version
```

Or build from a clone:

```sh
go build -o leasegrid ./cmd/leasegrid
```

## Usage

```sh
leasegrid --help
leasegrid list --help
leasegrid list --format json
leasegrid list --provider akash
leasegrid --config ./my-config.yaml list
```

Global flags such as `--config` go before the command.


Today, `leasegrid list` prints:

```text
leasegrid: no providers configured
```

Exit codes:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Error, such as a missing or invalid config file, no providers configured, or a provider failure |
| 2 | Invalid usage: unknown flag, command or output format |
| 130 | Interrupted with Ctrl+C |

## Configuration

LeaseGrid reads a YAML file:

```yaml
providers:
  - name: akash
    enabled: true
  - name: flux        # listed but disabled: enabled defaults to false
```

Where it looks:

| How | File | If the file is missing |
|---|---|---|
| `leasegrid --config PATH <command>` | `PATH` | Error, exit status 1 |
| No `--config` flag | `leasegrid/config.yaml` in your user config directory: `%AppData%` on Windows, `~/.config` on Linux, `~/Library/Application Support` on macOS | Built-in defaults: no providers |

Rules:

- An empty file is valid and enables nothing.
- Each provider needs a unique `name`.
- Unknown fields are rejected, so a typo such as `enabeld` is reported instead of being silently ignored.
- All validation problems are reported at once.
- The file never holds credentials.

No provider is implemented yet, so enabling one currently reports
`provider "akash" is not implemented yet`.

## Development


```sh
gofmt -l .           # must print nothing
go vet ./...
go test ./...
go test -race ./...  # needs cgo; on Windows, a C compiler such as gcc
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## Roadmap

1. ✅ CLI foundation
2. ✅ Lease model, provider interface, fake providers, and the `list` command
3. ✅ Configuration file
4. Akash provider integration
5. Table output

See [DESIGN.md](DESIGN.md) for the technical design.

## License

[MIT](LICENSE)
