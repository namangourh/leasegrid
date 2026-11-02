# Akash Adapter Contract

`internal/providers/akash` turns Akash's REST responses into LeaseGrid's
provider-neutral `lease.Lease`. It is the only code that knows Akash's
response shapes: nothing Akash-shaped leaves the package, so the service and
the CLI see leases and classified errors only.

Everything below is enforced by tests in that package and in
`cmd/leasegrid/akash_test.go`. No test needs network access or an Akash
account.

## Requests

LeaseGrid reads public chain data and sends no credentials. Lease data on
Akash is public, so the only setting is the owner's wallet address.

| Request | Path | Query | When |
|---|---|---|---|
| Lease list | `GET /akash/market/v1beta5/leases/list` | `filters.owner`, `pagination.limit=100`, `pagination.key` after the first page | always, up to 20 pages |
| Deployment info | `GET /akash/deployment/v1beta4/deployments/info` | `id.owner`, `id.dseq` | once per deployment with a lease that isn't closed |

- **Base URL:** `https://api.akashnet.net` unless the config sets `endpoint`.
  It must be `https`; certificate checks are never disabled.
- **Query values are escaped** with `url.Values.Encode`. An owner such as
  `akash1x&y=z+w #v` reaches the API as one intact value, with nothing cut
  off or injected.
- **Every lease record is validated before any deployment request**, so a bad
  lease list costs one request and no request is built from invalid data.

## Response shapes

The adapter decodes only the fields it uses. Unknown fields are ignored, so
fields Akash adds later don't break decoding. The shapes match
api.akashnet.net as observed in October 2026.

Lease list:

```json
{
  "leases": [
    {
      "lease": {
        "id": {
          "owner": "akash1...",
          "dseq": "1001",
          "gseq": 1,
          "oseq": 1,
          "provider": "akash1..."
        },
        "state": "active",
        "price": {"denom": "uakt", "amount": "10.000000000000000000"}
      }
    }
  ],
  "pagination": {"next_key": "cGFnZTI="}
}
```

`next_key` is empty or `null` on the last page. `price` is per block.

Deployment info:

```json
{
  "groups": [
    {
      "id": {"gseq": 1},
      "group_spec": {
        "name": "web",
        "resources": [
          {
            "resource": {
              "cpu": {"units": {"val": "500"}},
              "memory": {"quantity": {"val": "536870912"}},
              "storage": [{"quantity": {"val": "1073741824"}}],
              "gpu": {"units": {"val": "0"}}
            },
            "count": 1
          }
        ]
      }
    }
  ],
  "escrow_account": {
    "state": {"funds": [{"denom": "uact", "amount": "1800000.000000000000000000"}]}
  }
}
```

## Validation

A record that breaks any rule fails the whole call with
`providers.ErrBadResponse`. The message names the lease and the rule, never an
address or a raw value longer than 32 characters.

| Field | Rule | Message |
|---|---|---|
| `id.dseq` | present, a non-negative integer (sent as a string) | `missing id.dseq`, `invalid id.dseq` |
| `id.gseq`, `id.oseq` | present and non-zero | `missing id.gseq`, `missing id.oseq` |
| `id.owner` | equals the configured owner | `id.owner is not the configured owner` |
| `id.provider` | present | `missing id.provider` |
| `state` | one of the three below | `missing state`, `unsupported state "..."` |
| `price.denom` | present | `missing denom` |
| `price.amount` | a plain decimal: digits and at most one dot, at most 80 characters, not negative, no exponent | `missing amount`, `invalid amount` |
| price per hour | fits in an int64 | `price: amount out of range` |
| deployment group | a group with the lease's `gseq` exists | `deployment has no group N` |
| resource quantities | empty (meaning 0) or a non-negative integer | `invalid quantity` |

Afterwards `internal/app` checks every lease again against the provider
contract (`lease.Validate`, `Provider == "akash"`).

## Lease states

| Akash `state` | LeaseGrid status |
|---|---|
| `active` | `active` |
| `insufficient_funds` | `insufficient_funds` |
| `closed` | `closed` |
| empty | error: `missing state` |
| anything else | error: `unsupported state "..."` |

There is deliberately no `unknown` status. Akash's lease-state enum (market
`v1beta4` protobuf: `invalid` = 0, `active`, `insufficient_funds`, `closed`)
has no other real state: `invalid` is the zero value and marks a broken
record. An unrecognized state therefore fails the call instead of showing a
lease LeaseGrid can't interpret. If Akash adds a state, every command fails,
naming it, until the adapter maps it. The `v1beta5` enum LeaseGrid queries was
not re-checked and is assumed to be the same.

## Prices, denoms and expiry

- **Exact arithmetic.** Amounts are parsed into `math/big.Rat`, never
  `float64`. The per-hour cost is the per-block price × 600 (3600 s ÷ the
  6-second average block time), rounded half up to an integer in the denom's
  smallest unit.
- **Denoms are kept as given**, including long IBC denoms such as
  `ibc/170C...F2F1`. Amounts in different denoms are never added or converted.
- **Expiry is an estimate:** the deployment's escrow funds in the lease's
  denom ÷ what its open leases spend per block, × 6 s. It is unknown (empty)
  when there are no funds in that denom, nothing is being spent, or the
  estimate doesn't fit in a `time.Duration`. Closed leases get no expiry, name
  or resources, which saves one request per closed deployment.
- **Resources** are each profile's CPU millicores, memory, storage and GPUs
  times its count, summed per group.

## HTTP status classification

| Status | Category (`errors.Is`) | Message |
|---|---|---|
| 2xx | success | |
| 401, 403 | `providers.ErrUnauthorized` | `access denied: <request> returned HTTP 401` |
| 404, 501 | `providers.ErrNotFound` (501 means the API version was retired) | `endpoint not found: ...` |
| 429 | `providers.ErrRateLimited` | `rate limited: ...` |
| other 5xx | `providers.ErrUnavailable` | `provider unavailable: ...` |
| any other status | `providers.ErrBadResponse` | `bad response: ...` |
| network failure | `providers.ErrUnavailable` | the cause without the URL |

`<request>` is `lease list` or `deployment info`. The response body is never
read into an error.

## Timeouts, cancellation, limits and retries

- **One deadline per call:** 30 seconds by default covers the whole
  `ListLeases` call, every page and deployment request included. It then fails
  with an error matching `context.DeadlineExceeded` (exit status 1).
- **Cancellation:** the caller's context reaches every request. Ctrl+C, or a
  context already cancelled before the call, returns an error matching
  `context.Canceled` (exit status 130). An already-cancelled context sends no
  request at all.
- **Limits:** at most 20 pages of 100 leases (more is
  `akash.ErrTooManyPages`), and at most 8 MiB per response (more is
  `ErrBadResponse`). Every response body is closed.
- **No retries.** A failure is reported once. Whether to try again is the
  user's call; the matrix below gives the advice per case.

## Error layers

```
HTTP / JSON failure
  → adapter:  category + request, e.g. "rate limited: lease list returned HTTP 429"
  → service:  + provider, via %w:  "provider akash: rate limited: ..."
  → CLI:      "leasegrid: provider akash: rate limited: ..." on stderr; nothing on stdout
```

| Layer | Adds | Hides |
|---|---|---|
| `getJSON` (adapter) | the category sentinel, the request name, the HTTP status | the URL and query (owner address), the response body; transport errors are stripped of their URL |
| mapping (adapter) | the lease ID and the broken rule | addresses; values are quoted and cut to 32 characters |
| `app.Service` | `provider <name>:` | nothing new |
| `main` | the `leasegrid:` prefix, the exit code | nothing new |

Callers classify with `errors.Is` and never by matching strings. JSON decoding
errors keep `encoding/json`'s message, which can name a field or one offending
character but never echoes the body.

## Failure-contract matrix

Each row is a subtest of `TestAkashFailureContract`, run through
`leasegrid list` against a local TLS server. Every row also checks that stdout
is empty and that no output contains the response body, the owner address,
the query string or the server address.

| # | Case | Category | Retry? | Exit | stderr |
|---|---|---|---|---|---|
| 1 | Unknown lease state `"pending"` | `ErrBadResponse` | No: needs a LeaseGrid update | 1 | `leasegrid: provider akash: bad response: lease 1001/1/1: unsupported state "pending"` |
| 2 | HTTP 429 | `ErrRateLimited` | Yes, after waiting | 1 | `leasegrid: provider akash: rate limited: lease list returned HTTP 429` |
| 3 | Malformed JSON | `ErrBadResponse` | No, unless a proxy is at fault | 1 | `leasegrid: provider akash: bad response: lease list: invalid JSON: unexpected end of JSON input` |
| 4 | Context cancelled before the request | `context.Canceled` | No: the user stopped it | 130 | `leasegrid: provider akash: lease list: context canceled` |
| 5 | Cancelled during a slow request | `context.Canceled` | No: the user stopped it | 130 | `leasegrid: provider akash: lease list: context canceled` |
| 6 | HTTP 500 whose body holds `SECRET-MARKER token=abc123` | `ErrUnavailable` | Yes, later | 1 | `leasegrid: provider akash: provider unavailable: lease list returned HTTP 500` (body suppressed) |
| 7 | Owner `akash1x&y=z+w #v` | none: success | n/a | 0 | `No leases found.` (the owner arrived intact) |
| 8 | Per-hour cost beyond int64 | `ErrBadResponse` | No | 1 | `leasegrid: provider akash: bad response: lease 1001/1/1: price: amount out of range` |

A timeout (`context.DeadlineExceeded`, exit 1) is worth retrying when the
network is slow; it is tested by `TestListLeasesTimeout`.

## Fixtures

`internal/providers/akash/testdata` holds recorded responses, sanitized:
`akash1testowner000...` and `akash1testhost000...` replace real addresses,
and there is no real wallet, endpoint, key or credential.

| File | Content | Used for |
|---|---|---|
| `leases_page1.json` | leases `1001/1/1` (active, 5 uact per block) and `1002/1/1` (active, 10 uakt), with a `next_key` | paging, mapping, two denoms |
| `leases_page2.json` | lease `900/1/1` (closed, 2.5 uakt), last page | closed leases skip deployment requests |
| `deployment_1001.json` | group `web`, 0.5 CPU, 512 MiB, 1 GiB, escrow in uact | name, resources, expiry |
| `deployment_1002.json` | group `worker`: one profile with count 2 and two storage volumes (2 CPUs, 2 GiB, 12 GiB, 2 GPUs in total), escrow in uakt | multiplying by the count, summing volumes |
| `lease_malformed.json` | truncated JSON whose owner field is `SECRET-MARKER-must-not-appear-in-errors` | invalid JSON; leak checks |
| `lease_missing_field.json` | a lease without `id.dseq` | required fields |

The CLI tests in `cmd/leasegrid` serve the same files from a local TLS server.

## Live access

Live access is opt-in and never part of CI. `TestLiveAkash` is skipped unless
`LEASEGRID_AKASH_LIVE_OWNER` is set (and `LEASEGRID_AKASH_LIVE_URL`
optionally overrides the endpoint):

```sh
LEASEGRID_AKASH_LIVE_OWNER=akash1... go test ./internal/providers/akash -run TestLiveAkash -v
```

It logs the address you give it, the lease count, the counts by status and up
to five open leases. That is public chain data for a wallet address you
chose, and no credential is involved.

## Known gaps

- **`akash.ErrTooManyPages`** (more than 20 pages, about 2,000 leases) doesn't
  match any shared `providers` category, unlike every other failure. It exits
  with status 1 and is tested, but callers outside the package can't classify
  it with the shared sentinels.
- **The `v1beta5` lease-state enum** wasn't checked against Akash's sources;
  the mapping relies on the `v1beta4` definition.
- **No retries or backoff**, by design for now.
- **`show --format json`** is not implemented; `list --format json` is the
  machine-readable output.
