# Verification

How LeaseGrid's features are checked, and the evidence for each. Every result
below comes from a real command run; nothing here is a target or an estimate.

## Standard checks

Every push to `main` runs these in [CI](../.github/workflows/ci.yml) on
Ubuntu:

```sh
go mod tidy -diff
gofmt -l .            # must print nothing
go vet ./...
go test -race ./...
go build ./...
golangci-lint run     # v2.14
```

Locally (Windows, Go 1.26.3) the same checks run without `-race`, which needs
cgo and a C compiler, so the race detector is verified in CI only.

No test touches the network. Akash tests use sanitized recorded responses
(a test owner address, no real wallet, endpoint or credentials) served by a
local TLS server.

## `status` command

Shipped 2026-11-02 in four commits:

| Commit | Change | CI run |
|---|---|---|
| `b5dc3aa` | The `internal/summary` package | [41422033774](https://github.com/namangourh/leasegrid/actions/runs/41422033774) ✅ |
| `c60421e` | Text and JSON views for the dashboard | [41423336669](https://github.com/namangourh/leasegrid/actions/runs/41423336669) ✅ |
| `2056d20` | Color on a terminal | [41425998619](https://github.com/namangourh/leasegrid/actions/runs/41425998619) ✅ |
| `2052699` | The `status` command | [41430240805](https://github.com/namangourh/leasegrid/actions/runs/41430240805) ✅ |

Run 41430240805 passed every step: tidy, formatting, vet, tests with the
race detector, build and lint.

### Automated checks at `2052699`

| Check | Result |
|---|---|
| `go mod tidy -diff` | no diff |
| `gofmt -l .` | no files |
| `go vet ./...` | clean |
| `go build ./...` | ok |
| golangci-lint v2.14.0 | 0 issues |
| `go test ./... -count=1` | all packages pass; 451 passing tests and subtests, 0 failing (366 before this feature) |
| `go test ./... -count=20` | all packages pass |

Coverage (`go test ./... -cover`):

| Package | Coverage |
|---|---|
| `cmd/leasegrid` | 96.8% (`runStatus` 100%) |
| `internal/app` | 100.0% |
| `internal/config` | 100.0% |
| `internal/lease` | 100.0% |
| `internal/output` | 98.7% (all status and color code covered; the gap is in YAML rendering) |
| `internal/providers/akash` | 94.0% |
| `internal/providers/demo` | 100.0% |
| `internal/providers/fake` | 100.0% |
| `internal/summary` | 100.0% |

### What the tests cover

- **Calculations** (`internal/summary`): status and provider counts; the
  "expiring soon" boundaries (one second inside the window counts, the
  window's end and "expires now" don't, expired and unknown expiries don't,
  only active leases count); spend per denom over active leases only; a real
  zero cost; totals up to `math.MaxInt64` exactly; overflow as an error.
- **Rendering** (`internal/output`): golden tests for the text and JSON views,
  empty dashboards (`{}`/`[]`, never `null`), every count in its own place,
  `9223372036854775807` written digit for digit, control characters from
  provider data replaced in text and escaped in JSON, write errors returned.
- **Color:** the terminal decision (terminal, file, pipe, buffer, `NO_COLOR`
  set or empty, each mode); the color of each status and expiry, row by row;
  the window's effect on colors; and that removing the escape codes gives the
  plain dashboard byte for byte, so columns stay aligned.
- **Command** (`cmd/leasegrid`): a golden dashboard from the demo providers;
  the summary for each flag; JSON decoded into structs; invalid flags and
  arguments (exit 2, nothing on stdout, no provider called); one provider of
  two failing (exit 1, no partial dashboard); overflow; a missing config file;
  cancellation (exit 130); a dashboard from recorded Akash responses with two
  denoms; an Akash failure that must not reveal the owner, endpoint, query
  string or response body; and the built binary, whose piped output must have
  no escape codes.

### Deliberate-bug checks

To check that the tests catch real mistakes, bugs were planted in the code one
at a time and the tests run against each. A bug counts only if the code still
compiles.

| Part | Bugs planted | Caught |
|---|---|---|
| `internal/summary` | 11 | 11 |
| Text and JSON views | 20 | 20 |
| Color, and `summary.ExpiringSoon` | 20 | 20 |
| The `status` command | 16 | 15 |

The checks found three gaps in the tests, all fixed before commit:

- Swapping the `insufficient_funds` and `closed` counts went unnoticed,
  because the sample data had one of each. A test with all counts different
  was added.
- An unknown expiry shown in red went unnoticed: the only unknown expiry in
  the color test belonged to a closed lease, and the check searched the whole
  output instead of one row. The test now checks each row.
- `summary.ExpiringSoon` ignoring the lease status went unnoticed, because
  both callers filter by status first. It now has its own test.

The one bug still not caught automatically is **`--no-color` being ignored**.
Tests can't run the command on a real terminal: an in-process buffer and the
binary's pipe are never colored, so output looks the same either way. It is
covered by the manual check below.

### Smoke runs

The binary was built from `2052699` and run against
[`examples/demo.yaml`](../examples/demo.yaml) on 2026-11-02 with an empty user
config directory. Each prediction was written down before the run.

| # | Command (after `--config examples/demo.yaml`) | Predicted | Observed |
|---|---|---|---|
| 1 | `status` | exit 0; `5 (3 active, 1 insufficient_funds, 1 closed)`, `2 within 7d0h`, spend `4990 ibc/...` and `37800 uakt` per hour, `demo-a 3, demo-b 2`, five rows by expiry | as predicted |
| 2 | `status --format json` | exit 0; valid JSON, total 5, expiring_soon 2, `"168h0m0s"`, 2 spend entries, 5 leases, no escape byte | as predicted |
| 3 | `status --no-color` | identical to run 1 | identical, byte for byte |
| 4 | `status --provider demo-b` | `2 (1 active, 1 insufficient_funds, 0 closed)`, `1 within 7d0h`, `36000 uakt` only | as predicted |
| 5 | `status --status active` | `3 (3 active, ...)`, `2 within 7d0h`, both spend lines, `demo-a 2, demo-b 1` | as predicted |
| 6 | `status --expiring-within 168h` | same summary as run 1 | identical summary |
| 7 | `status --expiring-within 1h` | `0 within 1h0m`, still 5 leases (no filtering) | as predicted |
| 8 | `status --provider demo-b --status closed` | exit 0; zero counts, `-` for spend and providers, `No leases found.` on stdout | as predicted |
| 9 | `status --format yaml` | exit 2; `unknown output format "yaml" (supported: table, json)`; stdout empty | as predicted |
| 10 | `status --provider akahs` | exit 2; `unknown provider "akahs" (configured: demo-a, demo-b)`; stdout empty | as predicted |
| 11 | `status demo-a` | exit 2; `status takes no arguments, got ["demo-a"]`; stdout empty | as predicted |
| 12 | `status --expiring-within 0` | exit 2; `must be greater than zero`; stdout empty | as predicted |
| 13 | `status` with no config file | exit 1; `leasegrid: no providers configured`; stdout empty | as predicted |
| 14 | `status --help` | exit 0; help on stderr listing the five flags | as predicted |

14 of 14 matched. No run wrote an escape code to piped output.

### Manual color check

Run by hand on 2026-11-02 in the VS Code terminal on Windows 11 (PowerShell),
from a clone at `2052699`, with
`go run ./cmd/leasegrid --config examples/demo.yaml status`:

| Run | Result |
|---|---|
| `status` | Colored: `active` green, `insufficient_funds` red, `closed` gray; the two expiries 3 hours away yellow; the expired `insufficient_funds` lease's expiry in the normal color; columns aligned |
| `status --no-color` | Plain |
| `NO_COLOR=1`, then `status` | Plain |
| `status \| Out-String` (piped) | Plain |

### Limits

- **No live Akash call** was made for this feature; the Akash path is checked
  with recorded responses only.
- **The race detector** runs in CI only.
- **Old Windows consoles** that don't process escape codes would show them
  raw. This wasn't tested; `--no-color` or `NO_COLOR` is the workaround.
- **Not implemented:** charts, converting between denoms, and YAML output for
  `status`.

## Akash adapter failure contract

Hardened and documented on 2026-11-02 in three commits. The contract itself is
in [akash-adapter.md](akash-adapter.md).

| Commit | Change | CI run |
|---|---|---|
| `fd91399` | Leases must belong to the configured owner and name a provider | [41448704189](https://github.com/namangourh/leasegrid/actions/runs/41448704189) ✅ |
| `79eaf41` | Tests: owner URL escaping, per-hour cost overflow, a context cancelled before the request | [41448974467](https://github.com/namangourh/leasegrid/actions/runs/41448974467) ✅ |
| `9d414f4` | `TestAkashFailureContract`: eight failure cases end to end | [41449220762](https://github.com/namangourh/leasegrid/actions/runs/41449220762) ✅ |

Run 41449220762 passed every step: tidy, formatting, vet, tests with the race
detector, build and lint.

### Automated checks at `9d414f4`

| Check | Result |
|---|---|
| `go mod tidy -diff` | no diff |
| `gofmt -l .` | no files |
| `go vet ./...` | clean |
| `go build ./...` | ok |
| golangci-lint v2.14.0 | 0 issues |
| `go test ./... -count=1` | all packages pass; 468 passing tests and subtests, 0 failing (451 before) |
| `go test ./... -count=20` | all packages pass |
| Coverage, `internal/providers/akash` | 94.6% (94.0% before; the overflow branch is now covered) |
| Coverage, `cmd/leasegrid` | 96.8% |

### Failure-contract matrix

All eight rows of `TestAkashFailureContract` pass. Each one checks the error
category with `errors.Is`, the exit code, the exact message, an empty stdout,
and that no output contains the response body, the owner address, the query
string or the server address.

| # | Case | Exit | Observed message (after `leasegrid: `) |
|---|---|---|---|
| 1 | Unknown lease state | 1 | `provider akash: bad response: lease 1001/1/1: unsupported state "pending"` |
| 2 | HTTP 429 | 1 | `provider akash: rate limited: lease list returned HTTP 429` |
| 3 | Malformed JSON | 1 | `provider akash: bad response: lease list: invalid JSON: unexpected end of JSON input` |
| 4 | Cancelled before the request | 130 | `provider akash: lease list: context canceled` |
| 5 | Cancelled during a slow request | 130 | `provider akash: lease list: context canceled` |
| 6 | HTTP 500 with secrets in the body | 1 | `provider akash: provider unavailable: lease list returned HTTP 500` |
| 7 | Owner that needs URL escaping | 0 | (success: the owner arrived intact) |
| 8 | Per-hour cost beyond int64 | 1 | `provider akash: bad response: lease 1001/1/1: price: amount out of range` |

Two more examples, captured through `leasegrid list` against a local TLS server
at the same code:

```text
HTTP 401:               exit 1  leasegrid: provider akash: access denied: lease list returned HTTP 401
200 ms timeout reached: exit 1  leasegrid: provider akash: lease list: context deadline exceeded
```

The 401 response body held `SECRET-MARKER token=abc123`; neither appears.

### Deliberate-bug checks

| Part | Bugs planted | Caught |
|---|---|---|
| Owner and provider validation | 6 | 6 |
| Adapter gap tests | 5 | 5 |
| End-to-end matrix | 6 | 6 |

Among them: the owner not checked, the owner printed in an error, validation
skipped before deployment requests, the query left unescaped, the cost
overflow ignored, the request built without its context, 429 classified as
unavailable, the request URL or the response body put into an error, and
cancellation exiting 1. Every bug in the third part was caught by
`TestAkashFailureContract` itself.

### Findings and limits

- **`akash.ErrTooManyPages`** doesn't match any shared `providers` category,
  unlike every other failure. It was found while reviewing the error layers
  and is recorded as a known gap; it exits 1 and is tested.
- **No live Akash call** was made: the adapter is checked against recorded,
  sanitized responses only.
- **The binary can't be pointed at the fixtures:** it needs an HTTPS endpoint
  with a trusted certificate. The fixture-backed `list` and `show` paths
  (found, not found and failing) run in-process against a local TLS server
  instead (`TestShowAkash`, `TestShowAkashNotFound`, `TestStatusAkash` and the
  failure tests).
- **The race detector** runs in CI only.

## Release check (2026-11-02)

A checkpoint pass over the whole tool: every standard check, plus the built
binary run against the offline demo config. It was run on the Windows
development machine (Windows 11, Go 1.26.3) at `8a1c99a` plus one new test,
`TestShowEmptyProvider` (`show` when the provider returns no leases: exit 1,
`not found (searched: empty)`).

| Check | Result |
|---|---|
| `gofmt -l .` | no files |
| `go vet ./...` | clean |
| `go build ./...` | ok |
| `go mod tidy -diff` | no diff |
| golangci-lint v2.14.0 | 0 issues |
| `go test ./... -count=1` | all packages pass; 469 passing tests and subtests, 0 failing |
| `go test -cover ./...` | `cmd/leasegrid` 96.8%, `internal/output` 98.7%, `internal/providers/akash` 94.6%, every other package 100% |
| `go test -race ./...` | CI only: it needs cgo, and this machine has no C compiler |

The new test was checked by planting four bugs (not found reported as success,
the sentinel not wrapped, the searched providers left out, not found treated
as a usage error); it caught all four.

### Smoke runs

The binary was run from the repository root with an empty user config
directory. Each prediction was written down before the run.

| # | Command | Predicted | Observed | Exit |
|---|---|---|---|---|
| 1 | `leasegrid --help` | usage on stderr listing `list`, `show`, `status`, `-config`, `-version` | as predicted | 0 |
| 2 | `leasegrid --version` | `leasegrid 0.1.0` on stdout | as predicted | 0 |
| 3 | `leasegrid --config examples/demo.yaml list` | 5 rows: worker, web, gpu, api, batch | as predicted | 0 |
| 4 | `leasegrid --config examples/demo.yaml list --format json` | a JSON array of the same 5 leases | as predicted | 0 |
| 5 | `leasegrid --config examples/demo.yaml show 1002/1/1` | api, 1800 uakt, 1 CPU, 1 GiB, 5 GiB, 0 GPUs | as predicted | 0 |
| 6 | `leasegrid --config examples/demo.yaml show missing-id` | `leasegrid: lease "missing-id" not found (searched: demo-a, demo-b)` on stderr, stdout empty | as predicted | 1 |
| 7 | `leasegrid list --config examples/demo.yaml` | `flag provided but not defined: -config` and the `list` help; stdout empty | as predicted | 2 |

7 of 7 matched, with no escape codes in any output. Run 7 shows that
`--config` must go before the command.

### Notes

- **Fixture-backed runs use the demo providers.** The built binary needs an
  HTTPS endpoint with a trusted certificate, so it can't read the recorded
  Akash responses directly. Those run in-process against a local TLS server
  (see the Akash section above).
- **Code comments were reviewed:** every package has a package comment, there
  are no `TODO`s, and lint reports nothing, so no comments were changed.

## Earlier features

Their evidence is in the commit history and CI:

| Feature | Last commit | CI run |
|---|---|---|
| CLI foundation | `19e00e7` | [39399614213](https://github.com/namangourh/leasegrid/actions/runs/39399614213) ✅ |
| Configuration file | `80a11a8` | [39440880780](https://github.com/namangourh/leasegrid/actions/runs/39440880780) ✅ |
| Provider contract and Akash provider | `fdd4680` | [39950838030](https://github.com/namangourh/leasegrid/actions/runs/39950838030) ✅ |
| `list` with filters, sorting and demo providers | `fbd8add` | [41307512411](https://github.com/namangourh/leasegrid/actions/runs/41307512411) ✅ |
| `show` | `1c3e14f` | [41385250191](https://github.com/namangourh/leasegrid/actions/runs/41385250191) ✅ |
