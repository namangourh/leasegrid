# LeaseGrid Technical Design

## Goal

One CLI for viewing compute leases across DePIN providers, starting with Akash.

## Current State

- `leasegrid --help`, `leasegrid --version`
- `leasegrid [--config PATH] list` with `--format table|json|yaml`, `--provider`,
  `--status`, `--expiring-within` and `--sort-by`
- `leasegrid [--config PATH] show [--provider NAME] <id>`: one lease in detail
- `leasegrid [--config PATH] status` with `--format table|json`, `--provider`,
  `--status`, `--expiring-within` and `--no-color`: a dashboard of lease
  counts, leases expiring soon and spend per hour
- An Akash provider that reads real leases from Akash's public REST API,
  enabled from the config file with an owner wallet address
- Built-in demo providers (`demo-a`, `demo-b`) for trying LeaseGrid offline
- YAML configuration file: loaded, strictly parsed and validated
- One third-party dependency: `go.yaml.in/yaml/v3`, for the config file and
  YAML output
- In-memory fake providers for tests

## Next Slice

- Query providers concurrently, and show partial results when one fails

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
| `cmd/leasegrid` | Entry point: parses arguments, picks the config file, builds providers from it, prints output, maps errors to exit codes |
| `internal/app` | Use cases: declares the `Provider` interface, selects and calls providers, enforces their contract, merges, filters and sorts results, finds one lease by ID, labels errors |
| `internal/config` | Reads, strictly parses and validates the YAML config file; knows nothing about flags, specific providers or exit codes |
| `internal/lease` | Lease model (cost, resources, expiry), validation and pure helpers: `Filter`, `Sort` and the flag parsers `ParseStatus` and `ParseSortKey` |
| `internal/output` | Renders leases to any `io.Writer`: table, JSON and YAML through a `Formatter` interface, a one-lease detail view, and the status dashboard with its optional color |
| `internal/providers` | Error categories shared by every provider |
| `internal/providers/akash` | The Akash provider: REST requests, response mapping, error classification |
| `internal/providers/demo` | Built-in providers with fixed sample leases, for the offline demo |
| `internal/providers/fake` | Deterministic in-memory providers for tests |
| `internal/summary` | Pure calculations for the status dashboard: counts, "expiring soon" and exact spend totals per denom |

Dependencies point one way, so Go's no-import-cycles rule is never hit:

    cmd/leasegrid → app → lease, providers (error categories)
    cmd/leasegrid → summary → lease
    cmd/leasegrid → output → lease, summary
    cmd/leasegrid → config
    cmd/leasegrid → providers/akash → lease, providers
    cmd/leasegrid → providers/demo → lease

`internal/lease` imports no other LeaseGrid package. `cmd/leasegrid` is the
only package that knows the concrete providers; everything else sees the
`Provider` interface.

## The `list` Command

Data flows one way:

    flags ─► lease.Criteria + sort key ─► app.Service ─► Provider.ListLeases
          ─► []lease.Lease ─► lease.Filter ─► lease.Sort ─► output.Formatter ─► stdout

1. **Flags are validated first.** `--format`, `--status`, `--sort-by` and
   `--expiring-within` are parsed with `flag.Func` validators, so an invalid
   value is a usage error (exit 2) before the config is read or any provider
   is called.
2. **The clock is read once.** `runList` takes the current time once, in UTC
   and truncated to the second, and gives it to the providers (for expiry
   estimates), to the `--expiring-within` cutoff and to the table (for
   relative times). Tests inject a fixed clock, so output is byte-for-byte
   repeatable.
3. **The service** checks `--provider` against the configured names (an unknown
   name is a usage error), calls the selected providers one after another, and
   checks every lease against the provider contract.
4. **Filtering and sorting** are pure functions in `internal/lease`:
   - `Filter` keeps leases that match every criterion: provider, status, and
     a known expiry before the cutoff. Expired leases match the cutoff; leases
     with an unknown expiry never do.
   - `Sort` returns a sorted copy and never changes its input. Keys are `id`,
     `provider`, `status` (lifecycle order: active, insufficient_funds,
     closed) and `expires` (soonest first, unknown last; the default). Ties
     are broken by lease ID and then provider, because IDs are unique only
     within one provider. The sort is stable.
5. **Output** goes through `output.Formatter`:
   - **Table** (the default), built with `text/tabwriter`. EXPIRES shows UTC
     time and time left, such as `2026-11-01 15:44 UTC (in 3h0m)`. Long IBC
     denoms are shortened to `ibc/170C...F2F1`. Non-printable characters in
     provider-supplied text become `?`, so a lease name can't send escape
     sequences to the terminal.
   - **JSON** and **YAML** serialize the provider-neutral `lease.Lease`, never a
     provider's response types. Both use the same field names (`yaml` tags
     mirror `json` tags, enforced by a reflection test) and keep full denoms.
   - Every formatter builds its output in memory and writes it once, so a
     failure never leaves half a table or document behind.
6. **Empty results** exit 0. The table writes nothing to stdout and prints
   `No leases found.` on stderr; JSON and YAML write `[]`. stdout carries only
   data.

Exit codes: 0 success, 1 runtime error (config, no providers, provider
failure), 2 invalid usage, 130 interrupted.

## The `show` Command

    id + --provider ─► app.Service.FindLease ─► ListLeases (one or all providers)
                    ─► exact ID match ─► output.Detail ─► stdout

1. **Arguments are checked first.** `show` needs exactly one non-empty lease
   ID. Flags may go before or after it: `runShow` parses flags, takes the ID,
   and parses the rest again. A wrong number of IDs, an empty ID or an unknown
   flag is a usage error (exit 2) before the config is read.
2. **The lookup reuses listing.** `FindLease` calls `ListLeases` with the
   `--provider` selection, so provider selection, contract checks, error
   labels and cancellation work exactly as in `list`, and the `Provider`
   interface needs no new method. A lookup lists every searched provider in
   full; `--provider` limits it to one.
3. **Matching is exact**, and lease IDs are unique only within one provider:
   - one match is printed;
   - matches from several providers fail with `*app.AmbiguousLeaseError`,
     which lists them. The CLI suggests `--provider` and exits 2; the service
     never picks one;
   - no match fails with `app.ErrLeaseNotFound`, naming the providers
     searched, and exits 1;
   - the same ID twice from one provider is a bad response from that
     provider;
   - a provider error fails the lookup even if another provider matched,
     because the failing provider might have the ID too.
4. **The detail view**, `output.Detail`, prints ten `Label:  value` lines in a
   fixed order. Unlike the table, it keeps the full cost denom and shows
   resources in readable units (`0.5` CPUs, `512 MiB`, `1.5 GiB`). A value the
   provider didn't give is `-`, including all four resource lines when the
   provider reported no resources. It uses the same injected clock and the
   same control-character filter as the table.

`show` has no `--format` flag yet; `list --format json --provider NAME` is the
machine-readable path.

## The `status` Command

    flags ─► app.Service.ListLeases ─► summary.Calculate ─► output.WriteStatus ─► stdout

`runStatus` only coordinates: it parses flags, lists leases, calculates the
summary and renders it. The rules live in packages with no knowledge of flags
or exit codes.

1. **Flags are validated first**, as in `list`: `--format` accepts `table` or
   `json` (no YAML), `--status` the three statuses, `--expiring-within` a
   positive duration (default `168h`). `status` takes no positional arguments,
   so `status demo-a` (a forgotten `--provider`) is a usage error instead of an
   unfiltered dashboard.
2. **Listing reuses `ListLeases`** with the `--provider` and `--status`
   criteria and the default sort, so selection, contract checks, error labels
   and cancellation match `list`. Every number on the dashboard describes the
   leases shown. A failing provider fails the command with nothing on stdout:
   a dashboard missing one provider's leases would show wrong totals.
3. **`summary.Calculate(leases, now, window)`** is a pure function with the
   clock passed in:
   - counts per status and per provider;
   - **expiring soon** (`summary.ExpiringSoon`): an active lease whose known
     expiry is after now and before now plus the window. `--expiring-within`
     sets the window only; unlike in `list`, it never filters;
   - **spend per hour**: the sum of `Cost.PerHour` over active leases, one
     `lease.Cost` per denom, sorted by denom. A lease without a denom has no
     cost data and is skipped. Additions are checked:
     `c > math.MaxInt64 - total` returns `summary.ErrCostOverflow` (exit 1)
     instead of a wrapped-around number. Denoms are never converted.
   - The result never holds a nil map or slice, so JSON shows `{}` and `[]`.
4. **`output.WriteStatus`** renders one of two views:
   - **Text:** four `Label:  value` lines (`Leases`, `Expiring soon`,
     `Active spend` with full denoms, `By provider` sorted by name), a blank
     line, then the `list` table. With no leases, `No leases found.` replaces
     the table on stdout: the zero counts above it are data.
   - **JSON:** one object built from a separate view struct, so `summary`
     carries no JSON tags and the field names stay a stable contract:
     `as_of`, `expiring_within` (Go duration syntax), the counts,
     `by_provider`, `spend_per_hour` and `leases`.
5. **Color** (`output.ColorMode`: `ColorAuto`, `ColorAlways`, `ColorNever`)
   applies to the text view's STATUS and EXPIRES columns only. `ColorAuto`, the
   zero value, colors only when the writer is an `*os.File` whose `Stat`
   reports a character device and `NO_COLOR` is empty or unset; `--no-color`
   selects `ColorNever`. Statuses are green (active), red
   (insufficient_funds) and gray (closed); an active lease's expiry is red
   once passed and yellow while expiring soon. `text/tabwriter` counts escape
   codes as width, so every cell of a colored column, header included, is
   wrapped in a code of the same length; removing the codes gives the plain
   table byte for byte, which a test checks. JSON never has color. No
   terminal library is needed.

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
implicitly, and the compile-time checks live in their tests. Filtering and
sorting happen in `internal/app`, so every provider gets the same semantics.
`show` finds a lease through `ListLeases` too. A method such as `GetLease`
would be added only when a provider can look up one lease much more cheaply
than listing them all.

## Building Providers

`buildService` and `newProvider` in `cmd/leasegrid` turn each enabled config
entry into a provider. They are the program's composition root: the only code
that names concrete providers.

- `akash` becomes `akash.New(...)` with the entry's `owner` and `endpoint`.
  An empty endpoint means `https://api.akashnet.net`.
- `demo-a` and `demo-b` become demo providers. They take no settings, so an
  `owner` or `endpoint` on them is an error rather than being silently ignored.
- Any other name is an error that lists the available names.

Building a provider never touches the network; requests happen only when a
command lists leases.

## Akash Provider

`internal/providers/akash` reads one owner's leases from Akash's public REST API.
No credentials are involved, because lease data on Akash is public.

- **Requests:** `GET /akash/market/v1beta5/leases/list?filters.owner=…`, following
  `pagination.next_key` for at most 20 pages of 100, then
  `GET /akash/deployment/v1beta4/deployments/info` once per open deployment.
  Closed leases don't trigger deployment requests.
- **Validation:** every lease record must belong to the configured owner,
  name a provider, and have a valid ID, state (`active`, `insufficient_funds`
  or `closed`; anything else is a bad response) and price, before any
  deployment is requested.
- **Mapping:** the ID is `dseq/gseq/oseq`, and the name is the deployment's
  placement group name. Per-block prices become per-hour costs with exact
  `math/big` arithmetic (6-second blocks). Expiry is estimated from the
  deployment's escrow funds. Resources come from the lease's deployment group.
- **Safety:** one deadline per call (30 s by default), every response body closed,
  responses capped at 8 MiB, no retries, HTTPS only. Every lease record is
  validated before any further request. Errors never include request URLs,
  the owner address or response bodies. CLI tests check this for `list`,
  `show` and `status` against failing local TLS servers whose responses carry
  a secret marker, and for a request cancelled mid-flight.
- **Tests** run against sanitized fixtures in `testdata/`, served by a local
  `httptest` TLS server. No test touches the network unless you opt in to the
  live test in `live_test.go`. `TestAkashFailureContract` runs eight failure
  cases end to end through the CLI.

The full contract, including response shapes, validation rules, HTTP
classification and the failure matrix, is in
[docs/akash-adapter.md](docs/akash-adapter.md).

## Demo Providers

`internal/providers/demo` serves fixed leases under the names `demo-a` and
`demo-b`, enabled by ordinary config entries (see `examples/demo.yaml`). The
demo therefore runs through the same code as real providers: config,
`--provider`, contract checks, filtering, sorting and output.

The sample data covers every status, an unknown expiry, an expired lease, two
leases with the same expiry and the same lease ID under both providers, so the
sort tie-breakers are visible. Expiries are offsets from the injected clock,
so the demo always shows something about to expire, and tests pin the clock.

## Configuration

```yaml
providers:
  - name: akash
    enabled: true
    owner: akash1...
    endpoint: https://api.akashnet.net   # optional
```

- **Location:** `--config PATH`, which must exist; otherwise `leasegrid/config.yaml`
  in `os.UserConfigDir()`, which may be missing (giving built-in defaults).
- **Lazy loading:** only commands that need providers read the file, so
  `--version` and `--help` work even with a broken config.
- **Strict parsing:** unknown fields are rejected. An empty file is valid.
- **Provider-neutral:** every entry may carry `owner` and `endpoint`. The config
  package only parses them; `cmd/leasegrid` decides which provider uses them.
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
- **Deterministic output.** One injected clock reading per command, stable
  sorting with explicit tie-breakers, and map keys always sorted before
  output. Golden tests compare the table, the detail view and the status
  dashboard byte for byte, and the suite passes with `go test -count=20`.
- **stdout is for data.** Diagnostics go to stderr, so piping JSON or YAML
  into another tool is safe. `list` prints "No leases found." on stderr;
  `status` prints it on stdout as part of a dashboard that still shows its
  zero counts.
- **Cancellation everywhere.** Every provider call takes a `context.Context`.
  Ctrl+C cancels it, so a hung provider can't hang the CLI.
- **Errors are returned, not hidden.** Output functions return write errors, so a
  closed pipe is reported rather than silently losing output.
- **Read-only access.** Akash lease data is public, so LeaseGrid needs only a
  wallet address, never a private key.
- **Exact money.** Costs are integers in the token's smallest unit
  (1 AKT = 1,000,000 uakt), computed with exact `math/big` arithmetic, never
  floating point. Totals add costs only within one denom, with overflow
  checked; different denoms are never added, converted or compared.
- **Color is optional and plain text comes first.** Color only on a terminal,
  turned off by `--no-color` or `NO_COLOR`, and every status is still spelled
  out, so the dashboard reads the same without it.
- **Partial results (planned).** Providers are queried one after another, and
  the first failure stops the command. With a second real provider, one
  failing provider should not hide the others' leases; `status` will then
  have to mark an incomplete dashboard.
- **Strict configuration.** Unknown config fields are rejected, so typos are
  reported. Relaxing that later is non-breaking; tightening it would break
  existing files.
