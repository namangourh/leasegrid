# LeaseGrid Technical Design

## Goal

One CLI for viewing compute leases across DePIN providers, starting with Akash.

## Current State

- `leasegrid --help`, `leasegrid --version`
- `leasegrid [--config PATH] list [--format json] [--provider NAME]`
- YAML configuration file: loaded, strictly parsed and validated
- One third-party dependency: `go.yaml.in/yaml/v3`, for parsing the config file
- In-memory fake providers for tests; no real provider yet

## Next Slice

- Akash provider: read-only queries by owner wallet address
- Provider-specific settings in the config file, such as the Akash endpoint and
  owner address

## Non-Goals for Now

- Database persistence
- REST or gRPC API
- Credential storage
- Flux integration
- Alerts
- Caching
- Metrics and tracing

## Packages

| Package | Responsibility |
|---|---|
| `cmd/leasegrid` | Entry point: parses arguments, picks the config file, builds the service, prints output, maps errors to exit codes |
| `internal/app` | Use cases: selects providers, calls them, merges and filters results, labels errors |
| `internal/config` | Reads, strictly parses and validates the YAML config file; knows nothing about flags, providers or exit codes |
| `internal/providers` | The `Provider` interface |
| `internal/providers/fake` | Deterministic in-memory providers for tests |
| `internal/lease` | Lease model and pure helpers (`Filter`) |
| `internal/output` | Renders leases to any `io.Writer` (JSON for now) |

Dependencies point one way, so Go's no-import-cycles rule is never hit:

    cmd/leasegrid → app → providers → lease
    cmd/leasegrid → output → lease
    cmd/leasegrid → config

`internal/lease` imports no other LeaseGrid package.

## Provider Interface

```go
type Provider interface {
    Name() string
    List(ctx context.Context, criteria lease.Criteria) ([]lease.Lease, error)
}
```

Providers may ignore criteria they can't apply on the server side; `internal/app`
always filters the results again. Methods such as `GetLease` are added when a
command needs them.

## Configuration

```yaml
providers:
  - name: akash
    enabled: true
```

- **Location:** `--config PATH`, which must exist; otherwise `leasegrid/config.yaml`
  in `os.UserConfigDir()`, which may be missing (giving built-in defaults).
- **Lazy loading:** only commands that need providers read the file, so
  `--version` and `--help` work even with a broken config.
- **Strict parsing:** unknown fields are rejected. An empty file is valid.
- **Validation:** provider names are required and unique, and all problems are
  reported together.
- **Errors callers can classify:**
  - a missing file matches `fs.ErrNotExist`
  - anything wrong with the contents matches `config.ErrInvalid`
  - validation details are available as `*config.ValidationError`

  The CLI maps every config error to exit status 1.

## Key Design Choices

- **Thin `main`.** All logic lives in `run`, which returns errors instead of
  exiting. `main` alone turns errors into exit codes, so `run` is testable
  in-process.
- **Cancellation everywhere.** Every provider call takes a `context.Context`.
  Ctrl+C cancels it, so a hung provider can't hang the CLI.
- **Errors are returned, not hidden.** Output functions return write errors, so a
  closed pipe is reported rather than silently losing output.
- **Read-only access (planned).** Akash lease data is public, so LeaseGrid will
  need only a wallet address, never a private key.
- **Exact money (planned).** Costs will be integers in the token's smallest unit
  (1 AKT = 1,000,000 uakt), never floating point.
- **Partial results (planned).** With several providers, one failing provider
  should not hide the others' leases.
- **Strict configuration.** Unknown config fields are rejected, so typos are
  reported. Relaxing that later is non-breaking; tightening it would break
  existing files.

