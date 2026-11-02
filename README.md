# LeaseGrid

[![CI](https://github.com/namangourh/leasegrid/actions/workflows/ci.yml/badge.svg)](https://github.com/namangourh/leasegrid/actions/workflows/ci.yml)

LeaseGrid is a Go CLI for viewing compute leases across DePIN providers.

## Why

People who deploy on decentralized compute networks such as Akash and Flux need a
different tool for each network. Nothing gives them one view of all their active
leases, what they cost, or when they run out. LeaseGrid aims to be that single view.

## Status

Early development. What works today:

- `leasegrid list` reads real leases for any Akash wallet address from Akash's
  public API. No account, key or login is needed.
- It filters by provider, status and time to expiry, sorts by several keys, and
  prints a table, JSON or YAML.
- `leasegrid show <id>` prints one lease in detail.
- `leasegrid status` prints a dashboard: how many leases have each status, how
  many active leases expire soon, and what the active leases cost per hour.
- Built-in demo providers let you try every feature offline.
- A YAML configuration file, strictly parsed and validated.

Akash is the only real network so far.

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

## Try it offline

[`examples/demo.yaml`](examples/demo.yaml) enables two built-in demo providers,
`demo-a` and `demo-b`, which serve fixed sample leases. Nothing is sent over
the network. From a clone of this repository (use `go run ./cmd/leasegrid` in
place of `leasegrid` if you haven't installed it):

```sh
leasegrid --config examples/demo.yaml list
```

```text
ID        PROVIDER  NAME    STATUS              EXPIRES                                  COST/HOUR
1001/1/1  demo-b    worker  insufficient_funds  2026-11-01 10:44 UTC (expired 2h0m ago)  900 uakt
1001/1/1  demo-a    web     active              2026-11-01 15:44 UTC (in 3h0m)           4990 ibc/170C...F2F1
2001/1/1  demo-b    gpu     active              2026-11-01 15:44 UTC (in 3h0m)           36000 uakt
1002/1/1  demo-a    api     active              2026-11-11 12:44 UTC (in 10d0h)          1800 uakt
1003/1/1  demo-a    batch   closed              -                                        1200 uakt
```

The demo expiries are relative to the current time, so your dates will differ.
Some things to try:

```sh
# Active leases that expire within a day
leasegrid --config examples/demo.yaml list --status active --expiring-within 24h

# One provider's leases as YAML
leasegrid --config examples/demo.yaml list --provider demo-b --format yaml

# Everything as JSON, sorted by provider
leasegrid --config examples/demo.yaml list --format json --sort-by provider
```

To see one lease in detail, pass its ID to `show`:

```sh
leasegrid --config examples/demo.yaml show 1002/1/1
```

```text
ID:         1002/1/1
Provider:   demo-a
Name:       api
Status:     active
Expires:    2026-11-12 03:43 UTC (in 10d0h)
Cost/hour:  1800 uakt
CPU:        1
Memory:     1 GiB
Storage:    5 GiB
GPUs:       0
```

Lease IDs are unique only within one provider, and both demo providers have a
lease `1001/1/1`. Asking for it is ambiguous, so `show` asks you to choose:

```text
$ leasegrid --config examples/demo.yaml show 1001/1/1
leasegrid: lease "1001/1/1" is ambiguous (found in: demo-a, demo-b)
leasegrid: choose one with --provider NAME
```

Add `--provider demo-b`, before or after the ID, to see the `worker` lease.

For an overview, run `status`:

```sh
leasegrid --config examples/demo.yaml status
```

```text
Leases:         5 (3 active, 1 insufficient_funds, 1 closed)
Expiring soon:  2 within 7d0h
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                37800 uakt per hour
By provider:    demo-a 3, demo-b 2

ID        PROVIDER  NAME    STATUS              EXPIRES                                  COST/HOUR
1001/1/1  demo-b    worker  insufficient_funds  2026-11-02 08:40 UTC (expired 2h0m ago)  900 uakt
1001/1/1  demo-a    web     active              2026-11-02 13:40 UTC (in 3h0m)           4990 ibc/170C...F2F1
2001/1/1  demo-b    gpu     active              2026-11-02 13:40 UTC (in 3h0m)           36000 uakt
1002/1/1  demo-a    api     active              2026-11-12 10:40 UTC (in 10d0h)          1800 uakt
1003/1/1  demo-a    batch   closed              -                                        1200 uakt
```

In a terminal, statuses are colored, and so are the expiries of active leases
that have passed or expire soon.

## List your Akash leases

Create a config file with your Akash wallet address as `owner`:

```yaml
providers:
  - name: akash
    enabled: true
    owner: akash1...   # your wallet address; it's public, not a secret
```

Save it as `leasegrid/config.yaml` in your user config directory (see
[Configuration](#configuration)), or pass its path with `--config`:

```sh
leasegrid list
leasegrid list --expiring-within 168h   # leases that run out within a week
```

LeaseGrid only reads public chain data from `https://api.akashnet.net`, so it
never needs a private key.

Akash leases have no fixed end date: they run until the deployment's escrow
account runs out. LeaseGrid estimates EXPIRES from the escrow balance, the
lease prices and an average block time of 6 seconds, so treat it as an
estimate.

## Usage

```text
leasegrid [--config PATH] list [flags]
leasegrid [--config PATH] show [--provider NAME] <id>
leasegrid [--config PATH] status [flags]
```

Global flags such as `--config` go before the command.

### `list`

| `list` flag | Values | Default |
|---|---|---|
| `--format` | `table`, `json`, `yaml` | `table` |
| `--provider` | a configured provider name | all enabled providers |
| `--status` | `active`, `closed`, `insufficient_funds` | any status |
| `--expiring-within` | a Go duration such as `90m`, `24h` or `168h` (a week) | no limit |
| `--sort-by` | `id`, `provider`, `status`, `expires` | `expires` |

How it behaves:

- **Filters combine:** a lease must match all of them.
- **`--expiring-within`** keeps leases with a known expiry before now plus the
  duration, including leases that have already expired.
- **Sorting** is ascending. `expires` puts the soonest first and unknown expiries
  last, and `status` follows the lease lifecycle: active, insufficient_funds,
  closed. Ties are broken by lease ID and then provider, so the order never
  changes between runs.
- **The table** shows each expiry in UTC with the time left. Long IBC token
  denoms are shortened; JSON and YAML keep them in full. Amounts are integers in
  the token's smallest unit (1 AKT = 1,000,000 uakt).
- **An empty result** prints `No leases found.` on stderr in table mode, or `[]`
  in JSON and YAML. Either way it exits with status 0.
- **Every flag is checked** before any provider is called.
- **Provider errors** never include the endpoint, the owner address or the
  provider's raw response.

### `show`

`show` prints every field of one lease. Unlike the table, it shows the cost's
full denom and resources in readable units, and `-` for anything the provider
didn't report.

- **`--provider NAME`** searches only that provider. It may go before or after
  the ID.
- **An ID found under several providers** is an error that lists them; add
  `--provider` to choose one. `show` never picks one for you.
- **An ID that isn't found** prints `leasegrid: lease "<id>" not found
  (searched: ...)` and exits with status 1.
- **A provider that fails** fails the lookup, even if another provider has the
  ID, because the failing one might have it too.
- `show` prints text only. For machine-readable output, use
  `list --format json --provider NAME`.

### `status`

`status` summarizes the leases, then lists them in the same table as `list`,
soonest expiry first.

| `status` flag | Values | Default |
|---|---|---|
| `--format` | `table`, `json` | `table` |
| `--provider` | a configured provider name | all enabled providers |
| `--status` | `active`, `closed`, `insufficient_funds` | any status |
| `--expiring-within` | a Go duration such as `24h` | `168h` (a week) |
| `--no-color` | | color on a terminal |

How it behaves:

- **`--provider` and `--status` filter** the leases before they are counted, so
  every number describes the leases shown.
- **`--expiring-within` doesn't filter**, unlike in `list`. It sets the window
  for **Expiring soon**: active leases with a known expiry that is still in the
  future but within the window.
- **Active spend** adds up the per-hour cost of active leases, one total per
  denom. Closed leases cost nothing now, and an `insufficient_funds` lease has
  stopped paying. Denoms are never converted or added together, and totals are
  exact integers. A total too large to count exactly is an error, not a wrong
  number.
- **No leases** still prints the zero counts, followed by `No leases found.`
- **A provider that fails** fails the whole command, with nothing on stdout, so
  a total never silently leaves out a provider's leases.
- **Color** is used only when stdout is a terminal. `--no-color`, or a
  non-empty `NO_COLOR` environment variable, turns it off; piped output and
  JSON never have color. If an old Windows console shows codes such as `←[32m`
  instead of colors, use `--no-color`.
- **JSON** is one object for scripts: `as_of`, `expiring_within`, `total`,
  `active`, `insufficient_funds`, `closed`, `expiring_soon`, `by_provider`,
  `spend_per_hour` (a list of `{per_hour, denom}`) and `leases` (the same
  objects as `list --format json`). Empty values are `{}` or `[]`, never
  `null`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, including an empty result |
| 1 | Error, such as a missing or invalid config file, no providers configured, a provider failure, a lease `show` can't find, or a spend total too large to count exactly |
| 2 | Invalid usage: an unknown flag, command, format, status or sort key, a `--provider` name that isn't configured, an invalid duration, a missing or extra lease ID, an ID that several providers have, or an argument after `status` |
| 130 | Interrupted with Ctrl+C |

## Configuration

LeaseGrid reads a YAML file:

```yaml
providers:
  - name: akash
    enabled: true
    owner: akash1...
    endpoint: https://api.akashnet.net   # optional; this is the default
  - name: demo-a      # listed but disabled: enabled defaults to false
```

Where it looks:

| How | File | If the file is missing |
|---|---|---|
| `leasegrid --config PATH <command>` | `PATH` | Error, exit status 1 |
| No `--config` flag | `leasegrid/config.yaml` in your user config directory: `%AppData%` on Windows, `~/.config` on Linux, `~/Library/Application Support` on macOS | Built-in defaults: no providers |

Providers:

| Name | Settings |
|---|---|
| `akash` | `owner` (required): an `akash1...` wallet address. `endpoint` (optional): an `https` URL, default `https://api.akashnet.net` |
| `demo-a`, `demo-b` | None. Built-in sample data for trying LeaseGrid offline |

Rules:

- An empty file is valid and enables nothing.
- Each provider needs a unique `name`. An unknown name is an error that lists
  the available ones.
- Unknown fields are rejected, so a typo such as `enabeld` is reported instead of
  being silently ignored. A setting the provider doesn't use, such as `owner` on
  a demo provider, is rejected too.
- All validation problems in the file are reported at once.
- The file never holds credentials.

## Development

```sh
gofmt -l .           # must print nothing
go vet ./...
go test ./...
go test -race ./...  # needs cgo; on Windows, a C compiler such as gcc
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run
```

No test touches the network. Akash tests run against recorded responses served
by a local TLS server. An opt-in test runs against the real API; see
`internal/providers/akash/live_test.go`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for details, and
[docs/verification.md](docs/verification.md) for how each feature was verified.

## Roadmap

1. ✅ CLI foundation
2. ✅ Lease model, provider interface, fake providers, and the `list` command
3. ✅ Configuration file
4. ✅ Akash provider (tested against recorded API responses)
5. ✅ `list` with table, JSON and YAML output, filters, sorting, demo providers,
   and Akash wired in through the config file
6. ✅ `show`: one lease in detail, with ambiguous IDs reported instead of
   guessed
7. ✅ `status`: lease counts, leases expiring soon and spend per hour, with
   color on a terminal
8. Query several providers at once, with partial results when one fails

See [DESIGN.md](DESIGN.md) for the technical design.

## License

[MIT](LICENSE)
