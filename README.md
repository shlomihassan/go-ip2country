# go-ip2country

A REST service that resolves an IP address to a country and city. Built as a
take-home exercise, with an emphasis on clear, production-grade code: a
hand-rolled rate limiter (no third-party rate-limit library), a pluggable
datastore layer, and CI that actually proves the rate limiter works — not
just that the code compiles.

```bash
curl 'http://localhost:8080/v1/find-country?ip=2.22.233.255'
# {"country":"GBR","city":"London"}
```

## Contents

- [Quick start](#quick-start)
- [API](#api)
- [Configuration](#configuration)
- [How the rate limiter works](#how-the-rate-limiter-works)
- [Project structure](#project-structure)
- [Extending the datastore](#extending-the-datastore)
- [Tests](#tests)
- [Docker](#docker)
- [CI/CD (GitHub Actions)](#cicd-github-actions)

## Quick start

```bash
export DATASTORE_TYPE=csv
export DATASTORE_CSV_PATH=testdata/ip2country.csv
export RATE_LIMIT_GLOBAL_RPS=100
export RATE_LIMIT_PER_IP_RPS=10
export SERVER_PORT=8080

go run ./cmd/server
```

## API

| Endpoint | Description |
|---|---|
| `GET /v1/find-country?ip=<ip>` | Looks up an IP. Success: `200 {"country":"XXX","city":"xxx"}`. |
| `GET /healthz` | Liveness check for containers/orchestrators. Always `200`, never rate limited. |

Errors are always `{"error":"..."}` with a matching status code:

| Condition | Status |
|---|---|
| Missing `ip` query param | `400` |
| `ip` not a valid IP address | `400` |
| No location found for that IP | `404` |
| Rate limit exceeded (global or per-IP) | `429` |
| Unexpected internal error | `500` |

## Configuration

Everything is driven by environment variables, validated once at startup —
if anything's missing or invalid, the process logs why and exits immediately
rather than serving traffic in a half-broken state.

| Variable | Required | Default | Description |
|---|---|---|---|
| `SERVER_PORT` | no | `8080` | TCP port the HTTP server listens on. |
| `DATASTORE_TYPE` | no | `csv` | Selects the datastore implementation. |
| `DATASTORE_CSV_PATH` | yes, when `DATASTORE_TYPE=csv` | — | Path to the CSV dataset (`ip,city,country` per line). |
| `RATE_LIMIT_GLOBAL_RPS` | yes | — | Requests/sec budget shared across *all* clients combined. |
| `RATE_LIMIT_PER_IP_RPS` | yes | — | Requests/sec budget for *each individual* client IP. |
| `LOG_LEVEL` | no | `info` | One of `debug`, `info`, `warn`, `error`. |

## How the rate limiter works

Every request has to pass **two independent checks**, back to back:

1. **Global limiter** — one shared counter for the whole service. Protects
   the service itself from being overwhelmed, no matter who's asking.
2. **Per-IP limiter** — a separate counter for each client IP. Stops any
   single caller from hogging the whole budget.

Both use the same simple algorithm — a **fixed window counter**: count
requests in the current 1-second block, and reset the count when a new
second starts. It's the simplest correct way to hand-roll a rate limiter
(the accepted trade-off is that a client can burst up to ~2x the limit right
at the boundary between two windows — deliberately not solved here, since a
sliding window or token bucket would add real complexity for a marginal
gain in this context).

Because many requests can arrive at the exact same instant, the counter is
protected by a mutex so concurrent requests can never race past the limit
(see `internal/ratelimit/fixedwindow.go`). A small background goroutine also
periodically evicts old per-IP counters so memory doesn't grow forever as
new IPs show up.

This isn't just described in a doc — it's verified two different ways in
CI:
- A Go-level integration test spins up the real router with real limiters
  in-process and checks the second request gets a `429`
  (`internal/httpapi/integration_test.go`).
- A GitHub Actions workflow builds the actual Docker image, runs it as a
  real container, and hits it over real HTTP twice to confirm the same
  behavior end-to-end (see [CI/CD](#cicd-github-actions) below).

## Project structure

```
go-ip2country/
├── cmd/server/          # wiring: config → locator → limiters → router → server; graceful shutdown
├── internal/
│   ├── config/          # env var parsing & validation, fails fast on bad config
│   ├── geo/             # datastore abstraction (Locator interface + registry)
│   │   └── csv/         # CSV-backed Locator implementation
│   ├── ratelimit/       # the hand-rolled fixed-window rate limiter
│   └── httpapi/         # chi router, handlers, rate-limit middleware, JSON responses
├── testdata/            # sample IP→country/city CSV used by tests and local runs
├── .github/workflows/   # CI: tests, Docker build, GHCR publish, end-to-end rate-limit check
├── Dockerfile
└── go.mod
```

## Extending the datastore

The datastore is pluggable via a small `database/sql`-style registry, so
adding a new data source never touches `main.go`, the handlers, or routing.

Implement `geo.Locator` in a new package and register it in an `init()`:

```go
func init() {
    geo.Register("my-datastore", New)
}
```

Then set `DATASTORE_TYPE=my-datastore` and blank-import the package from
`cmd/server/main.go`. See `internal/geo/csv` for a complete example.

## Tests

```bash
go test -race ./...
```

`-race` matters here specifically because of the rate limiter's shared,
concurrently-accessed state — it's the same flag CI uses.

## Docker

```bash
docker build -t go-ip2country:local .
docker run --rm -p 8080:8080 \
  -e RATE_LIMIT_GLOBAL_RPS=100 -e RATE_LIMIT_PER_IP_RPS=10 \
  go-ip2country:local
```

## CI/CD (GitHub Actions)

Three workflows live in `.github/workflows/`, all runnable automatically on
push/PR and manually via the **"Run workflow"** button on GitHub's Actions
tab:

| Workflow | What it does |
|---|---|
| `test.yml` | Runs `go test -race ./...` — the full unit + integration test suite, including the in-process rate-limit test. |
| `docker.yml` | Waits for `test.yml` to pass, then builds the Docker image (on every push/PR, to catch a broken `Dockerfile` early). **Only** when that build is a push to `master` does it also log in and publish the image to GitHub Container Registry, tagged with both the commit SHA and `latest` — `ghcr.io/shlomihassan/go-ip2country`. |
| `e2e-rate-limit.yml` | The most "real" check: builds the Docker image, runs it as an actual container, and fires two HTTP requests at it with a tight per-IP limit — asserting the first succeeds (`200`) and the second is rejected (`429`). This is the closest thing to a reviewer manually curling the service twice and watching it work. |

**Why gate the GHCR push behind tests, but not the build?** A broken
`Dockerfile` is useful to catch on every PR (cheap, fast feedback). Actually
*publishing* an image, though, should never happen unless the code behind it
is known-good — hence `docker.yml`'s publish step only fires after
`test.yml` succeeds, and only for `master`.
