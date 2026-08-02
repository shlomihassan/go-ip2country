# IP-to-Country Service — Design

## Goal

A REST service that resolves an IP address to a country/city, with a hand-rolled
request-rate-limiting mechanism and a datastore layer that's easy to extend to new
ip2country data sources. Built for a take-home coding exercise; "production-grade
quality" per the brief.

## Requirements (from exercise brief)

1. Clear, readable code.
2. Configuration via environment variables.
3. `GET /v1/find-country?ip=<ip>` → `200 {"country":"XXX","city":"xxx"}`.
4. Errors → JSON `{"error":"XXX"}` with an appropriate HTTP status code.
5. Datastore is pluggable; active implementation selected via env var.
6. Hand-rolled rate limiter (no `golang.org/x/time/rate` or any other rate-limit
   library); limit configured via env var; over-limit → `429`.
7. Production-grade delivery.
8. Bonus: tests.
9. Bonus: Docker build script.

## Decisions

| Area | Decision | Rationale |
|---|---|---|
| Rate limit scope | **Layered**: a global limiter and a per-client-IP limiter are both always active. A request must pass both. | Matches the spec's literal "request per second limiting mechanism" (global) while also being realistic for a public API (per-IP), without making it configurable/switchable — simpler, always-correct behavior. |
| Rate limit algorithm | **Fixed window counter** (count requests in the current 1s wall-clock window, reset each window). | Simplest correct hand-rolled implementation; boundary double-burst is an accepted, documented tradeoff for this exercise. |
| IP matching | **Exact match** against a `map[string]Location` loaded fully into memory at startup. | Matches the spec's example CSV format (`ip,city,country`) directly; O(1) lookup; simplest correct implementation. Real-world range-based datasets are out of scope. |
| Datastore extensibility | `geo.Locator` interface + a `database/sql`-style **registry**: each implementation self-registers under a name (e.g. `"csv"`) in an `init()`. `DATASTORE_TYPE` selects the active one at startup. | Adding a new datastore later = new package + one `Register()` call. No changes to `main.go`, handlers, or routing. |
| HTTP routing | **chi router**. | Clean middleware chaining for the two rate-limit layers + logging; still lightweight. |
| Logging | **`log/slog`** (stdlib, JSON handler). | Structured logs, zero extra dependency. |
| Tests | **testify** (`assert`/`require`). | Standard, readable Go test idiom. |
| Client IP extraction | `r.RemoteAddr` (TCP peer), not `X-Forwarded-For`. | `X-Forwarded-For` is client-supplied and trivially spoofable without a trusted proxy in front — using it would make the per-IP limiter bypassable. No reverse proxy is specified for this exercise, so the TCP peer address is the only trustworthy source. |
| Go version | 1.26 (as installed). | Latest available toolchain. |
| Module path | `go-ip2country` (no VCS host prefix). | This is a standalone binary, not intended to be imported by other modules. |

## Architecture

```
go-ip2country/
├── cmd/server/main.go        # wiring: config → locator → limiters → router → server; graceful shutdown
├── internal/
│   ├── config/                # env var parsing & validation, fails fast on bad config
│   │   └── config.go
│   ├── geo/                   # datastore abstraction
│   │   ├── geo.go             # Location, Locator interface, ErrNotFound, registry
│   │   └── csv/
│   │       └── csv.go         # CSV-backed Locator, self-registers as "csv"
│   ├── ratelimit/
│   │   └── fixedwindow.go     # Limiter interface + FixedWindow implementation (used for both global and per-IP)
│   └── httpapi/
│       ├── router.go          # chi router + middleware wiring
│       ├── handlers.go        # GET /v1/find-country
│       ├── middleware.go      # rate-limit middleware (wraps a ratelimit.Limiter + key func)
│       └── respond.go         # JSON success/error response helpers
├── testdata/
│   └── ip2country.csv         # sample dataset used by tests and the default config
├── Dockerfile
├── go.mod
└── README.md
```

### `geo` package

```go
type Location struct {
    Country string
    City    string
}

var ErrNotFound = errors.New("location not found")

type Locator interface {
    Lookup(ip net.IP) (Location, error)
}

type Factory func(cfg config.Config) (Locator, error)

func Register(name string, f Factory)
func New(name string, cfg config.Config) (Locator, error) // looks up registry, error if unknown name
```

`internal/geo/csv` implements `Locator` by loading the whole CSV into a
`map[string]Location` (keyed by the normalized IP string) at construction time, and
registers itself via `init() { geo.Register("csv", New) }`. Malformed lines fail
the load with a descriptive error (fail fast at startup, not at request time).

CSV format (no header row): `ip,city,country` — e.g. `2.22.233.255,London,GBR`.

### `ratelimit` package

```go
type Limiter interface {
    Allow(key string) bool
}

func NewFixedWindow(limit int, window time.Duration) *FixedWindow
func (f *FixedWindow) Allow(key string) bool
func (f *FixedWindow) Close() // stops the background sweeper goroutine
```

One implementation serves both roles:
- **Global limiter**: constructed once, every call uses the same fixed key (`""`) —
  effectively one shared counter.
- **Per-IP limiter**: constructed once, calls use the caller's IP as the key —
  one counter per IP, held in an internal `map[string]*windowCounter` guarded by a
  mutex.

To bound memory growth from the per-IP map, `FixedWindow` runs a background sweeper
(`time.Ticker`, interval = 10× the window) that evicts counters whose window has
expired. `Close()` stops the ticker; `main.go` calls it during graceful shutdown.

### `httpapi` package

- `router.go` builds the chi router: global rate-limit middleware → per-IP
  rate-limit middleware → route table. Per-request access logging is not
  included (out of scope for the exercise — easy to add later as another chi
  middleware); `main.go`'s `slog` logger already covers startup, shutdown, and
  configuration/datastore errors.
- `middleware.go` defines one rate-limit middleware constructor,
  `RateLimit(limiter ratelimit.Limiter, keyFunc func(*http.Request) string) func(http.Handler) http.Handler`,
  used twice: once with a keyFunc returning `""` (global), once returning the
  request's remote IP (per-IP).
- `handlers.go`: `FindCountry(locator geo.Locator) http.HandlerFunc` — parses the
  `ip` query param, validates with `net.ParseIP`, calls `locator.Lookup`, writes the
  response.
- `respond.go`: `writeJSON(w, status, v any)` and `writeError(w, status, msg string)`
  helpers producing `{"error": "..."}`.

### Error mapping

| Condition | Status | Body |
|---|---|---|
| Missing `ip` query param | 400 | `{"error":"missing required query parameter: ip"}` |
| `ip` not a valid IP address | 400 | `{"error":"invalid ip address"}` |
| No location found for IP | 404 | `{"error":"location not found for ip"}` |
| Global or per-IP rate limit exceeded | 429 | `{"error":"rate limit exceeded"}` |
| Unexpected internal error | 500 | `{"error":"internal server error"}` |

### Configuration (environment variables)

| Variable | Required | Default | Notes |
|---|---|---|---|
| `SERVER_PORT` | no | `8080` | TCP port the HTTP server listens on. |
| `DATASTORE_TYPE` | no | `csv` | Selects the `geo.Locator` implementation via the registry. |
| `DATASTORE_CSV_PATH` | yes, when `DATASTORE_TYPE=csv` | — | Path to the CSV dataset. |
| `RATE_LIMIT_GLOBAL_RPS` | yes | — | Global requests/sec budget; must be a positive integer. |
| `RATE_LIMIT_PER_IP_RPS` | yes | — | Per-client-IP requests/sec budget; must be a positive integer. |
| `LOG_LEVEL` | no | `info` | One of `debug`, `info`, `warn`, `error`. |

`internal/config` parses and validates all of this once at startup; any error
(missing required var, non-integer, non-positive limit, unknown log level) causes
the process to log the problem and `os.Exit(1)` before the server starts — no
partially-configured server ever comes up.

### Operational details

- `http.Server` sets `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, and
  `ReadHeaderTimeout` (all short, e.g. 5–15s) to avoid slow-client resource
  exhaustion.
- `GET /healthz` returns `200 OK` unconditionally (no rate limiting, no datastore
  dependency) for container/orchestrator liveness checks.
- Graceful shutdown: `main.go` listens for `SIGINT`/`SIGTERM`, calls
  `srv.Shutdown(ctx)` with a bounded timeout, then closes both rate limiters.

### Docker

A multi-stage `Dockerfile`: a `golang:1.26-alpine` builder stage producing a static
binary, copied into a minimal `alpine` (or `scratch` + certs) final stage alongside
`testdata/ip2country.csv`. Default `ENV` values are supplied for the non-secret
config vars; the container `EXPOSE`s `SERVER_PORT` and runs the binary as
`ENTRYPOINT`.

## Testing strategy

- `internal/ratelimit`: unit tests for `FixedWindow` — allows up to the limit within
  a window, blocks over-limit, allows again after the window rolls over, and keeps
  independent counts per key. Time is controlled by injecting a `now func() time.Time`
  (defaulting to `time.Now`) so tests never sleep on the real clock.
- `internal/geo/csv`: unit tests for successful load + lookup (hit and miss), and
  for load-time failure on a malformed CSV line.
- `internal/httpapi`: handler tests via `httptest`, table-driven over the error
  mapping table above, using a fake `geo.Locator`; middleware tests for the 429 path
  using a fake `ratelimit.Limiter`.
- One end-to-end test wiring the real chi router, real CSV locator (against
  `testdata/ip2country.csv`), and real rate limiters behind `httptest.NewServer`,
  exercising the full request path including a 429 once the limit is exceeded.

## Out of scope

- IP range/CIDR datasets (spec's example format is single-IP rows).
- IPv6 datasets in `testdata` (the `Locator` interface and CSV loader work with any
  `net.IP` string form, but the sample dataset only needs IPv4 to satisfy the
  exercise's example).
- Authentication/authorization — not requested by the brief.
- Distributed/shared rate limiting (e.g. Redis-backed) — single-process in-memory
  limiter is sufficient for the exercise and keeps the "no external rate-limit
  library" constraint trivially satisfied.
