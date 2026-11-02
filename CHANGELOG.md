# Changelog

Notable changes to LeaseGrid. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [1.0.0] - 2026-11-02

The first release. Everything below is new.

### Commands

- `leasegrid list`: every lease from the configured providers, as a table,
  JSON or YAML (`--format`), filtered by provider, status and time to expiry
  (`--provider`, `--status`, `--expiring-within`) and sorted by ID, provider,
  status or expiry (`--sort-by`, soonest expiry first by default). Sorting is
  deterministic, with explicit tie-breakers.
- `leasegrid show <id>`: one lease in detail, with the full cost denom and
  readable resources. An ID that several providers share is reported as
  ambiguous, never guessed; `--provider` resolves it.
- `leasegrid status`: lease counts per status and provider, active leases
  expiring soon (`--expiring-within`, a week by default), and the per-hour
  spend of active leases as exact totals per denom, followed by the leases.
  Table or JSON (`--format`). Color on a terminal only; `--no-color` or
  `NO_COLOR` turns it off.
- `--config PATH`, `--version` and `--help`.

### Providers

- Akash: reads one wallet's leases from Akash's public REST API
  (`https://api.akashnet.net` by default; the config's `endpoint` can change
  it). Needs only the public wallet address, never a key. Costs are converted
  exactly from per-block prices to per hour, and expiry is estimated from the
  escrow balance.
- `demo-a` and `demo-b`: built-in providers with fixed sample leases, for
  trying LeaseGrid offline with `examples/demo.yaml`.

### Configuration

- A YAML file, at `--config PATH` or `leasegrid/config.yaml` in the user
  config directory. It is strictly parsed: unknown fields, duplicate names and
  settings a provider doesn't use are rejected, and all problems are reported
  together. It never holds credentials.

### Reliability and safety

- Failures are classified (access denied, not found, rate limited,
  unavailable, bad response) and reported on stderr with the provider and the
  request, never the endpoint, wallet address, query string or response body.
- Exit status 0 on success, 1 on errors, 2 on invalid usage, 130 when
  interrupted with Ctrl+C, which cancels requests in flight.
- Every provider response is validated before use. Akash leases must belong
  to the configured owner, and an unknown lease state is an error rather than
  a guess.
- One 30-second deadline per Akash call, responses capped at 8 MiB, at most
  2,000 leases (20 pages), and no automatic retries.
- Provider-supplied text can't inject terminal escape sequences.

### Known limitations

- Akash is the only real network.
- Providers are queried one after another, and one failing provider fails the
  command.
- No conversion between denoms, no charts, no YAML for `status`, and no
  `show --format`.

[1.0.0]: https://github.com/namangourh/leasegrid/releases/tag/v1.0.0
