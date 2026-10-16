# LeaseGrid Technical Design

## MVP Goal

Provide one CLI for viewing compute leases across DePIN providers,
starting with Akash.

## First Public Slice

- `leasegrid --help`
- `leasegrid --version`
- Standard-library Go CLI
- Table-driven subprocess smoke tests
- GitHub Actions format, lint, test, and build checks

## Next Slice

- `leasegrid list --provider akash`
- Provider interface
- Normalized lease model
- Mock provider tests

## Non-Goals for the First Slice

- Database persistence
- REST or gRPC API
- Credential storage
- Akash integration
- Flux integration
- Alerts
- Caching
- Metrics and tracing

## Initial Package Boundaries

- `cmd/leasegrid`: process entry point and CLI commands
- `internal/lease`: lease domain model and pure helpers (filter, sort, totals)
- `internal/providers`: provider interface and implementations
- `internal/aggregator`: queries providers concurrently and merges results
- `internal/config`: configuration loading
- `internal/output`: output formatting

Dependencies point one way, so Go's no-import-cycles rule is never hit:

    cmd/leasegrid → aggregator → providers → lease

`internal/lease` imports no other LeaseGrid package.

## Key Design Choices

- **Read-only access.** Akash lease data is public, so LeaseGrid needs only a
  wallet address, never a private key.
- **Exact money.** Costs are integers in the token's smallest unit
  (1 AKT = 1,000,000 uakt), never floating point.
- **Partial results.** If one provider is down, LeaseGrid still shows the
  others and reports which one failed.
