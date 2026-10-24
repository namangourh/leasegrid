# LeaseGrid Technical Design

## Goal

One CLI for viewing compute leases across DePIN providers, starting with Akash.

## Current State

- `leasegrid --help`, `leasegrid --version`
- `leasegrid [--config PATH] list [--format json] [--provider NAME]`
- YAML configuration file: loaded, strictly parsed and validated
- One third-party dependency: `go.yaml.in/yaml/v3`, for parsing the config file
- An Akash provider, tested against sanitized recordings of the real API, but
  not yet wired into the CLI
- In-memory fake providers for tests

## Next Slice

- Wire the Akash provider into `leasegrid list`
- Provider-specific settings in the config file: the Akash endpoint and owner
  address

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
| `internal/app` | Use cases: declares the `Provider` interface, selects and calls providers, enforces their contract, merges and filters results, labels errors |
| `internal/config` | Reads, strictly parses and validates the YAML config file; knows nothing about flags, providers or exit codes |
| `internal/providers` | Error categories shared by every provider |
| `internal/providers/akash` | The Akash provider: REST requests, response mapping, error classification |
| `internal/providers/fake` | Deterministic in-memory providers for tests |
| `internal/lease` | Lease model (cost, resources, expiry), validation and pure helpers (`Filter`) |
| `internal/output` | Renders leases to any `io.Writer` (JSON for now) |

Dependencies point one way, so Go's no-import-cycles rule is never hit:

    cmd/leasegrid → app → lease, providers (error categories)
    cmd/leasegrid → output → lease
    cmd/leasegrid → config
    providers/akash → lease, providers     (not yet wired into the CLI)

`internal/lease` imports no other LeaseGrid package.

## Provider Contract

The interface is declared by its consumer, `internal/app`:

```go
type Provider interface {
    Name() string
    ListLeases(ctx context.Context) ([]lease.Lease, error)
}
```

A provider guarantees that:

- every returned lease passes `lease.Validate()` and has `Provider == Name()`
  (`internal/app` checks this, and reports violations as a bad response);
- it returns promptly once `ctx` is done, with an error matching
  `context.Canceled` or `context.DeadlineExceeded`;
- other failures match one of the shared categories in `internal/providers`:

| Error | Meaning |
|---|---|
| `ErrUnauthorized` | Access refused (HTTP 401 or 403) |
| `ErrNotFound` | Endpoint missing (404) or API version retired (501) |
| `ErrRateLimited` | Too many requests (429) |
| `ErrUnavailable` | Provider down or unreachable (other 5xx, network errors) |
| `ErrBadResponse` | Malformed JSON, missing or invalid fields, unexpected status |

Implementations never import `internal/app`: Go interfaces are satisfied
implicitly, and the compile-time checks live in their tests. Filtering by
provider and status happens in `internal/app`. Methods such as `GetLease` are
added when a command needs them.

## Akash Provider

`internal/providers/akash` reads one owner's leases from Akash's public REST API.
No credentials are involved, because lease data on Akash is public.

- **Requests:** `GET /akash/market/v1beta5/leases/list?filters.owner=…`, following
  `pagination.next_key` for at most 20 pages of 100, then
  `GET /akash/deployment/v1beta4/deployments/info` once per open deployment.
  Closed leases don't trigger deployment requests.
- **Mapping:** the ID is `dseq/gseq/oseq`. Per-block prices become per-hour costs
  with exact `math/big` arithmetic (6-second blocks). Expiry is estimated from
  the deployment's escrow funds. Resources come from the lease's deployment group.
- **Safety:** one deadline per call (30 s by default), every response body closed,
  responses capped at 8 MiB, no retries, HTTPS only. Every lease record is
  validated before any further request. Errors never include request URLs or
  response bodies.
- **Tests** run against sanitized fixtures in `testdata/`, served by a local
  `httptest` TLS server. No test touches the network.

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
- **Read-only access.** Akash lease data is public, so LeaseGrid needs only a
  wallet address, never a private key.
- **Exact money.** Costs are integers in the token's smallest unit
  (1 AKT = 1,000,000 uakt), computed with exact `math/big` arithmetic, never
  floating point.
- **Partial results (planned).** With several providers, one failing provider
  should not hide the others' leases.
- **Strict configuration.** Unknown config fields are rejected, so typos are
  reported. Relaxing that later is non-breaking; tightening it would break
  existing files.

