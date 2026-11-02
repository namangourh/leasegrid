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

## Earlier features

Their evidence is in the commit history and CI:

| Feature | Last commit | CI run |
|---|---|---|
| CLI foundation | `19e00e7` | [39399614213](https://github.com/namangourh/leasegrid/actions/runs/39399614213) ✅ |
| Configuration file | `80a11a8` | [39440880780](https://github.com/namangourh/leasegrid/actions/runs/39440880780) ✅ |
| Provider contract and Akash provider | `fdd4680` | [39950838030](https://github.com/namangourh/leasegrid/actions/runs/39950838030) ✅ |
| `list` with filters, sorting and demo providers | `fbd8add` | [41307512411](https://github.com/namangourh/leasegrid/actions/runs/41307512411) ✅ |
| `show` | `1c3e14f` | [41385250191](https://github.com/namangourh/leasegrid/actions/runs/41385250191) ✅ |
