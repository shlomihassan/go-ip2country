# IP-to-Country Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go REST service exposing `GET /v1/find-country?ip=<ip>` that resolves an IP to a country/city from a pluggable datastore, protected by a hand-rolled global + per-IP rate limiter, per the design in `docs/superpowers/specs/2026-08-02-ip2country-design.md`.

**Architecture:** Small, independently-testable packages behind interfaces (`geo.Locator`, `ratelimit.Limiter`) wired together in `cmd/server/main.go`. A `database/sql`-style registry lets new datastores self-register. chi handles routing; two rate-limit middlewares (global, per-IP) share one `FixedWindow` implementation.

**Tech Stack:** Go 1.26, `github.com/go-chi/chi/v5`, `github.com/stretchr/testify`, `log/slog` (stdlib). No rate-limiting library of any kind.

## Global Constraints

- No open-source rate-limiting library, including `golang.org/x/time/rate` (exercise requirement — spec item 6).
- Config only via environment variables (spec item 2) — never flags or config files.
- Success response: exactly `{"country": "XXX", "city": "xxx"}` (spec item 3).
- Error response: exactly `{"error": "XXX"}` with an appropriate HTTP status (spec item 4).
- Rate-limit exceeded → HTTP 429 (spec item 6).
- Module path: `go-ip2country`. All internal imports use this prefix (e.g. `go-ip2country/internal/config`).
- Every package below lives under `internal/` except `cmd/server` — this is a standalone binary, not a library other modules import.

---

### Task 1: Config package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  ```go
  package config

  type Config struct {
      ServerPort         string
      DatastoreType      string
      DatastoreCSVPath   string
      RateLimitGlobalRPS int
      RateLimitPerIPRPS  int
      LogLevel           string
  }

  func Load(getenv func(string) string) (Config, error)
  ```
  `getenv` is injected (rather than calling `os.Getenv` directly) so tests never touch real process environment state.

- [ ] **Step 1: Add testify dependency**

Run: `go get github.com/stretchr/testify`

- [ ] **Step 2: Write the failing test for the happy path**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoad_AppliesDefaultsAndParsesRequiredValues(t *testing.T) {
	getenv := fakeEnv(map[string]string{
		"DATASTORE_CSV_PATH":    "testdata/foo.csv",
		"RATE_LIMIT_GLOBAL_RPS": "100",
		"RATE_LIMIT_PER_IP_RPS": "10",
	})

	cfg, err := config.Load(getenv)

	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.ServerPort)
	assert.Equal(t, "csv", cfg.DatastoreType)
	assert.Equal(t, "testdata/foo.csv", cfg.DatastoreCSVPath)
	assert.Equal(t, 100, cfg.RateLimitGlobalRPS)
	assert.Equal(t, 10, cfg.RateLimitPerIPRPS)
	assert.Equal(t, "info", cfg.LogLevel)
}
```

- [ ] **Step 3: Run test, verify it fails**

Run: `go test ./internal/config/...`
Expected: build failure — `undefined: config.Load` (the package doesn't exist yet). This is the expected RED for a not-yet-implemented package.

- [ ] **Step 4: Write minimal implementation for the happy path**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"strconv"
)

type Config struct {
	ServerPort         string
	DatastoreType      string
	DatastoreCSVPath   string
	RateLimitGlobalRPS int
	RateLimitPerIPRPS  int
	LogLevel           string
}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		ServerPort:    envOrDefault(getenv, "SERVER_PORT", "8080"),
		DatastoreType: envOrDefault(getenv, "DATASTORE_TYPE", "csv"),
		LogLevel:      envOrDefault(getenv, "LOG_LEVEL", "info"),
	}

	if cfg.DatastoreType == "csv" {
		cfg.DatastoreCSVPath = getenv("DATASTORE_CSV_PATH")
	}

	globalRPS, err := parsePositiveInt(getenv, "RATE_LIMIT_GLOBAL_RPS")
	if err != nil {
		return Config{}, err
	}
	cfg.RateLimitGlobalRPS = globalRPS

	perIPRPS, err := parsePositiveInt(getenv, "RATE_LIMIT_PER_IP_RPS")
	if err != nil {
		return Config{}, err
	}
	cfg.RateLimitPerIPRPS = perIPRPS

	return cfg, nil
}

func envOrDefault(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

func parsePositiveInt(getenv func(string) string, key string) (int, error) {
	raw := getenv(key)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %d", key, n)
	}
	return n, nil
}
```

- [ ] **Step 5: Run test, verify it passes**

Run: `go test ./internal/config/...`
Expected: PASS

- [ ] **Step 6: Write the failing test for validation errors**

Append to `internal/config/config_test.go`:

```go
func TestLoad_RejectsInvalidInput(t *testing.T) {
	base := map[string]string{
		"DATASTORE_CSV_PATH":    "testdata/foo.csv",
		"RATE_LIMIT_GLOBAL_RPS": "100",
		"RATE_LIMIT_PER_IP_RPS": "10",
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing global rps", func(v map[string]string) { delete(v, "RATE_LIMIT_GLOBAL_RPS") }},
		{"non-integer global rps", func(v map[string]string) { v["RATE_LIMIT_GLOBAL_RPS"] = "fast" }},
		{"zero global rps", func(v map[string]string) { v["RATE_LIMIT_GLOBAL_RPS"] = "0" }},
		{"negative per-ip rps", func(v map[string]string) { v["RATE_LIMIT_PER_IP_RPS"] = "-1" }},
		{"missing csv path for csv datastore", func(v map[string]string) { delete(v, "DATASTORE_CSV_PATH") }},
		{"invalid log level", func(v map[string]string) { v["LOG_LEVEL"] = "verbose" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := make(map[string]string, len(base))
			for k, v := range base {
				values[k] = v
			}
			tt.mutate(values)

			_, err := config.Load(fakeEnv(values))

			assert.Error(t, err)
		})
	}
}
```

- [ ] **Step 7: Run test, verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL — subtests `zero global rps`, `negative per-ip rps`, `missing csv path for csv datastore`, and `invalid log level` fail (no error returned), because `Load` doesn't yet validate these.

- [ ] **Step 8: Extend implementation to cover validation**

In `internal/config/config.go`, add a package-level var and two checks inside `Load`:

```go
var validLogLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
```

Replace the log-level assignment block with:

```go
	cfg.LogLevel = envOrDefault(getenv, "LOG_LEVEL", "info")
	if !validLogLevels[cfg.LogLevel] {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: must be one of debug, info, warn, error", cfg.LogLevel)
	}
```

Replace the datastore-path block with:

```go
	if cfg.DatastoreType == "csv" {
		cfg.DatastoreCSVPath = getenv("DATASTORE_CSV_PATH")
		if cfg.DatastoreCSVPath == "" {
			return Config{}, fmt.Errorf("DATASTORE_CSV_PATH is required when DATASTORE_TYPE=csv")
		}
	}
```

(`parsePositiveInt` already rejects missing/non-integer/non-positive values, so the RPS cases pass unchanged.)

- [ ] **Step 9: Run test, verify it passes**

Run: `go test ./internal/config/...`
Expected: PASS (both test functions, all subtests)

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/config
git commit -m "feat: add config package with env var loading and validation"
```

---

### Task 2: Rate limiter (fixed window)

**Files:**
- Create: `internal/ratelimit/fixedwindow.go`
- Test: `internal/ratelimit/fixedwindow_test.go`

**Interfaces:**
- Produces:
  ```go
  package ratelimit

  type Limiter interface {
      Allow(key string) bool
  }

  func NewFixedWindow(limit int, window time.Duration) *FixedWindow
  func (f *FixedWindow) Allow(key string) bool
  func (f *FixedWindow) Close()
  ```
  `*FixedWindow` implements `Limiter`. One instance serves the global limiter (always called with key `""`); a second instance serves the per-IP limiter (called with the caller's IP as key).

- [ ] **Step 1: Write the failing test for limit enforcement and per-key independence**

Create `internal/ratelimit/fixedwindow_test.go`:

```go
package ratelimit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/ratelimit"
)

func TestFixedWindow_AllowsUpToLimitThenBlocks(t *testing.T) {
	f := ratelimit.NewFixedWindow(2, time.Second)
	defer f.Close()

	assert.True(t, f.Allow("a"))
	assert.True(t, f.Allow("a"))
	assert.False(t, f.Allow("a"), "third request in the same window should be blocked")
}

func TestFixedWindow_TracksKeysIndependently(t *testing.T) {
	f := ratelimit.NewFixedWindow(1, time.Second)
	defer f.Close()

	assert.True(t, f.Allow("a"))
	assert.True(t, f.Allow("b"), "a different key should have its own budget")
	assert.False(t, f.Allow("a"), "key a is already at its limit")
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/ratelimit/...`
Expected: build failure — `undefined: ratelimit.NewFixedWindow`

- [ ] **Step 3: Write minimal implementation**

Create `internal/ratelimit/fixedwindow.go`:

```go
package ratelimit

import (
	"sync"
	"time"
)

type Limiter interface {
	Allow(key string) bool
}

type windowCounter struct {
	count       int
	windowStart time.Time
}

type FixedWindow struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	counters map[string]*windowCounter
	now      func() time.Time
	stop     chan struct{}
}

func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	return newFixedWindow(limit, window, time.Now)
}

func newFixedWindow(limit int, window time.Duration, now func() time.Time) *FixedWindow {
	f := &FixedWindow{
		limit:    limit,
		window:   window,
		counters: make(map[string]*windowCounter),
		now:      now,
		stop:     make(chan struct{}),
	}
	go f.sweepLoop()
	return f
}

func (f *FixedWindow) Allow(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now()
	c, ok := f.counters[key]
	if !ok || now.Sub(c.windowStart) >= f.window {
		c = &windowCounter{count: 0, windowStart: now}
		f.counters[key] = c
	}

	if c.count >= f.limit {
		return false
	}
	c.count++
	return true
}

func (f *FixedWindow) sweepLoop() {
	ticker := time.NewTicker(f.window * 10)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.sweep(f.now())
		case <-f.stop:
			return
		}
	}
}

func (f *FixedWindow) sweep(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, c := range f.counters {
		if now.Sub(c.windowStart) >= 2*f.window {
			delete(f.counters, key)
		}
	}
}

func (f *FixedWindow) Close() {
	close(f.stop)
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/ratelimit/...`
Expected: PASS

- [ ] **Step 5: Write the failing test for window rollover**

Append to `internal/ratelimit/fixedwindow_test.go` (this test needs the unexported constructor, so it must live in package `ratelimit`, not `ratelimit_test` — add a second file):

Create `internal/ratelimit/fixedwindow_internal_test.go`:

```go
package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFixedWindow_ResetsAfterWindowRollsOver(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }

	f := newFixedWindow(1, time.Second, clock)
	defer f.Close()

	assert.True(t, f.Allow("a"))
	assert.False(t, f.Allow("a"), "still within the same window")

	current = current.Add(time.Second)
	assert.True(t, f.Allow("a"), "new window should reset the budget")
}

func TestFixedWindow_SweepEvictsExpiredCounters(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }

	f := newFixedWindow(1, time.Second, clock)
	defer f.Close()

	f.Allow("a")
	assert.Len(t, f.counters, 1)

	current = current.Add(3 * time.Second) // >= 2*window
	f.sweep(current)

	assert.Empty(t, f.counters, "expired counters should be evicted by sweep")
}
```

- [ ] **Step 6: Run test, verify it fails**

Run: `go test ./internal/ratelimit/...`
Expected: FAIL — `TestFixedWindow_ResetsAfterWindowRollsOver` fails because the last assertion gets `false` (window logic not yet exercised with the clock), OR both new tests actually build and the rollover behavior already works since `Allow` already checks `now.Sub(c.windowStart) >= f.window` — **verify by running**; if it already passes, note this in the commit and move on (the sweep test is the one expected to newly exercise `sweep`, which is already implemented too). If both already pass because Step 3's implementation already included this logic, that's fine — these tests still lock the behavior in as a regression guard; proceed straight to Step 8.

- [ ] **Step 7: Adjust implementation only if Step 6 showed a failure**

The implementation from Step 3 already includes window-rollover and sweep logic, so no code change is expected here. If a failure did occur, fix `Allow` or `sweep` in `internal/ratelimit/fixedwindow.go` to match the test expectations above.

- [ ] **Step 8: Run test, verify it passes**

Run: `go test ./internal/ratelimit/...`
Expected: PASS (all four test functions)

- [ ] **Step 9: Commit**

```bash
git add internal/ratelimit
git commit -m "feat: add hand-rolled fixed-window rate limiter"
```

---

### Task 3: `geo` package — Locator interface and datastore registry

**Files:**
- Create: `internal/geo/geo.go`
- Test: `internal/geo/geo_test.go`

**Interfaces:**
- Consumes: `config.Config` (from Task 1, `go-ip2country/internal/config`)
- Produces:
  ```go
  package geo

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
  func New(name string, cfg config.Config) (Locator, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/geo/geo_test.go`:

```go
package geo_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
)

type fakeLocator struct{}

func (fakeLocator) Lookup(net.IP) (geo.Location, error) {
	return geo.Location{Country: "USA", City: "Testville"}, nil
}

func TestRegisterAndNew(t *testing.T) {
	geo.Register("geo-test-fake", func(cfg config.Config) (geo.Locator, error) {
		return fakeLocator{}, nil
	})

	locator, err := geo.New("geo-test-fake", config.Config{})
	require.NoError(t, err)

	loc, err := locator.Lookup(net.ParseIP("1.2.3.4"))
	require.NoError(t, err)
	assert.Equal(t, geo.Location{Country: "USA", City: "Testville"}, loc)
}

func TestNew_UnknownDatastoreType(t *testing.T) {
	_, err := geo.New("does-not-exist", config.Config{})
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/geo/...`
Expected: build failure — `undefined: geo.Register` (package doesn't exist yet)

- [ ] **Step 3: Write minimal implementation**

Create `internal/geo/geo.go`:

```go
package geo

import (
	"errors"
	"fmt"
	"net"

	"go-ip2country/internal/config"
)

type Location struct {
	Country string
	City    string
}

var ErrNotFound = errors.New("location not found")

type Locator interface {
	Lookup(ip net.IP) (Location, error)
}

type Factory func(cfg config.Config) (Locator, error)

var registry = map[string]Factory{}

func Register(name string, f Factory) {
	registry[name] = f
}

func New(name string, cfg config.Config) (Locator, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown datastore type %q", name)
	}
	return f(cfg)
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/geo/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/geo/geo.go internal/geo/geo_test.go
git commit -m "feat: add geo.Locator interface and datastore registry"
```

---

### Task 4: CSV-backed Locator

**Files:**
- Create: `internal/geo/csv/csv.go`
- Test: `internal/geo/csv/csv_test.go`
- Create: `internal/geo/csv/testdata/valid.csv`
- Create: `internal/geo/csv/testdata/malformed.csv`
- Create: `testdata/ip2country.csv` (repo-root sample dataset used by the running service and the end-to-end test in Task 6)

**Interfaces:**
- Consumes: `geo.Location`, `geo.ErrNotFound`, `geo.Locator`, `geo.Register` (Task 3); `config.Config` (Task 1)
- Produces: `func New(cfg config.Config) (geo.Locator, error)` — matches `geo.Factory`; self-registers as `"csv"` via `init()`.

- [ ] **Step 1: Create fixture files**

Create `internal/geo/csv/testdata/valid.csv`:

```
2.22.233.255,London,GBR
8.8.8.8,Mountain View,USA
```

Create `internal/geo/csv/testdata/malformed.csv`:

```
2.22.233.255,London
```

Create `testdata/ip2country.csv` (repo root):

```
2.22.233.255,London,GBR
8.8.8.8,Mountain View,USA
1.1.1.1,Sydney,AUS
```

- [ ] **Step 2: Write the failing test**

Create `internal/geo/csv/csv_test.go`:

```go
package csv_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
	geocsv "go-ip2country/internal/geo/csv"
)

func TestNew_LoadsAndLooksUpEntries(t *testing.T) {
	locator, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/valid.csv"})
	require.NoError(t, err)

	loc, err := locator.Lookup(net.ParseIP("8.8.8.8"))
	require.NoError(t, err)
	assert.Equal(t, geo.Location{Country: "USA", City: "Mountain View"}, loc)
}

func TestLookup_NotFound(t *testing.T) {
	locator, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/valid.csv"})
	require.NoError(t, err)

	_, err = locator.Lookup(net.ParseIP("9.9.9.9"))
	assert.ErrorIs(t, err, geo.ErrNotFound)
}
```

- [ ] **Step 3: Run test, verify it fails**

Run: `go test ./internal/geo/csv/...`
Expected: build failure — `undefined: geocsv.New` (package doesn't exist yet)

- [ ] **Step 4: Write minimal implementation**

Create `internal/geo/csv/csv.go`:

```go
package csv

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
)

func init() {
	geo.Register("csv", New)
}

type Locator struct {
	entries map[string]geo.Location
}

func New(cfg config.Config) (geo.Locator, error) {
	return load(cfg.DatastoreCSVPath)
}

func load(path string) (*Locator, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening csv datastore %q: %w", path, err)
	}
	defer f.Close()

	entries := make(map[string]geo.Location)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: expected 3 fields (ip,city,country), got %d", path, lineNum, len(fields))
		}
		ipStr := strings.TrimSpace(fields[0])
		city := strings.TrimSpace(fields[1])
		country := strings.TrimSpace(fields[2])

		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("%s:%d: invalid ip address %q", path, lineNum, ipStr)
		}
		entries[ip.String()] = geo.Location{City: city, Country: country}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading csv datastore %q: %w", path, err)
	}

	return &Locator{entries: entries}, nil
}

func (l *Locator) Lookup(ip net.IP) (geo.Location, error) {
	loc, ok := l.entries[ip.String()]
	if !ok {
		return geo.Location{}, geo.ErrNotFound
	}
	return loc, nil
}
```

- [ ] **Step 5: Run test, verify it passes**

Run: `go test ./internal/geo/csv/...`
Expected: PASS

- [ ] **Step 6: Write the failing test for load-time errors**

Append to `internal/geo/csv/csv_test.go`:

```go
func TestNew_MalformedCSVFailsToLoad(t *testing.T) {
	_, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/malformed.csv"})
	assert.Error(t, err)
}

func TestNew_MissingFileFailsToLoad(t *testing.T) {
	_, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/does-not-exist.csv"})
	assert.Error(t, err)
}
```

- [ ] **Step 7: Run test, verify it fails**

Run: `go test ./internal/geo/csv/...`
Expected: for `valid.csv`/`malformed.csv`/missing-file cases, these should actually already **pass** given Step 4's implementation (it already validates field count and IP parsing, and `os.Open` already errors on a missing file). Run it to confirm; if any subtest unexpectedly passes without the corresponding check, that's a sign Step 4 needs the check added first — but per Step 4's code above, both checks already exist, so this step is a verification run, not a fresh RED.

- [ ] **Step 8: Run test, verify it passes**

Run: `go test ./internal/geo/csv/...`
Expected: PASS (all four test functions)

- [ ] **Step 9: Commit**

```bash
git add internal/geo/csv testdata/ip2country.csv
git commit -m "feat: add CSV-backed geo.Locator implementation"
```

---

### Task 5: HTTP API — handlers, rate-limit middleware, router

**Files:**
- Create: `internal/httpapi/respond.go`
- Create: `internal/httpapi/handlers.go`
- Create: `internal/httpapi/middleware.go`
- Create: `internal/httpapi/router.go`
- Test: `internal/httpapi/handlers_test.go`
- Test: `internal/httpapi/middleware_test.go`
- Test: `internal/httpapi/router_test.go`

**Interfaces:**
- Consumes: `geo.Locator`, `geo.Location`, `geo.ErrNotFound` (Task 3/4); `ratelimit.Limiter` (Task 2)
- Produces:
  ```go
  package httpapi

  func FindCountry(locator geo.Locator) http.HandlerFunc
  func RateLimit(limiter ratelimit.Limiter, keyFunc func(*http.Request) string) func(http.Handler) http.Handler
  func GlobalKey(r *http.Request) string
  func PerIPKey(r *http.Request) string
  func NewRouter(locator geo.Locator, globalLimiter, perIPLimiter ratelimit.Limiter) http.Handler
  ```

- [ ] **Step 1: Add chi dependency**

Run: `go get github.com/go-chi/chi/v5`

- [ ] **Step 2: Write the failing test for `FindCountry`**

Create `internal/httpapi/handlers_test.go`:

```go
package httpapi_test

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/httpapi"
)

type fakeLocator struct {
	loc geo.Location
	err error
}

func (f fakeLocator) Lookup(net.IP) (geo.Location, error) {
	return f.loc, f.err
}

func TestFindCountry(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		locator    fakeLocator
		wantStatus int
		wantBody   string
	}{
		{
			name:       "success",
			url:        "/v1/find-country?ip=8.8.8.8",
			locator:    fakeLocator{loc: geo.Location{Country: "USA", City: "Mountain View"}},
			wantStatus: http.StatusOK,
			wantBody:   `{"country":"USA","city":"Mountain View"}` + "\n",
		},
		{
			name:       "missing ip param",
			url:        "/v1/find-country",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"missing required query parameter: ip"}` + "\n",
		},
		{
			name:       "invalid ip",
			url:        "/v1/find-country?ip=not-an-ip",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid ip address"}` + "\n",
		},
		{
			name:       "not found",
			url:        "/v1/find-country?ip=9.9.9.9",
			locator:    fakeLocator{err: geo.ErrNotFound},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"location not found for ip"}` + "\n",
		},
		{
			name:       "internal error",
			url:        "/v1/find-country?ip=9.9.9.9",
			locator:    fakeLocator{err: errors.New("boom")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rec := httptest.NewRecorder()

			httpapi.FindCountry(tt.locator)(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		})
	}
}
```

- [ ] **Step 3: Run test, verify it fails**

Run: `go test ./internal/httpapi/...`
Expected: build failure — `undefined: httpapi.FindCountry`

- [ ] **Step 4: Write minimal implementation**

Create `internal/httpapi/respond.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
```

Create `internal/httpapi/handlers.go`:

```go
package httpapi

import (
	"errors"
	"net"
	"net/http"

	"go-ip2country/internal/geo"
)

type findCountryResponse struct {
	Country string `json:"country"`
	City    string `json:"city"`
}

func FindCountry(locator geo.Locator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ipParam := r.URL.Query().Get("ip")
		if ipParam == "" {
			writeError(w, http.StatusBadRequest, "missing required query parameter: ip")
			return
		}

		ip := net.ParseIP(ipParam)
		if ip == nil {
			writeError(w, http.StatusBadRequest, "invalid ip address")
			return
		}

		loc, err := locator.Lookup(ip)
		if errors.Is(err, geo.ErrNotFound) {
			writeError(w, http.StatusNotFound, "location not found for ip")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, findCountryResponse{Country: loc.Country, City: loc.City})
	}
}
```

- [ ] **Step 5: Run test, verify it passes**

Run: `go test ./internal/httpapi/...`
Expected: PASS

- [ ] **Step 6: Write the failing test for the rate-limit middleware**

Create `internal/httpapi/middleware_test.go`:

```go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/httpapi"
)

type fakeLimiter struct{ allow bool }

func (f fakeLimiter) Allow(string) bool { return f.allow }

func TestRateLimit_AllowsWhenLimiterAllows(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := httpapi.RateLimit(fakeLimiter{allow: true}, httpapi.GlobalKey)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimit_BlocksWhenLimiterDenies(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	mw := httpapi.RateLimit(fakeLimiter{allow: false}, httpapi.GlobalKey)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.False(t, called)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, `{"error":"rate limit exceeded"}`+"\n", rec.Body.String())
}
```

- [ ] **Step 7: Run test, verify it fails**

Run: `go test ./internal/httpapi/...`
Expected: build failure — `undefined: httpapi.RateLimit`, `undefined: httpapi.GlobalKey`

- [ ] **Step 8: Write minimal implementation**

Create `internal/httpapi/middleware.go`:

```go
package httpapi

import (
	"net"
	"net/http"

	"go-ip2country/internal/ratelimit"
)

func RateLimit(limiter ratelimit.Limiter, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(keyFunc(r)) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func GlobalKey(_ *http.Request) string {
	return ""
}

func PerIPKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

- [ ] **Step 9: Run test, verify it passes**

Run: `go test ./internal/httpapi/...`
Expected: PASS

- [ ] **Step 10: Write the failing test for the router**

Create `internal/httpapi/router_test.go`:

```go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/httpapi"
)

func TestNewRouter_HealthCheck(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: true}, fakeLimiter{allow: true})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRouter_FindCountryRoute(t *testing.T) {
	router := httpapi.NewRouter(
		fakeLocator{loc: geo.Location{Country: "USA", City: "Mountain View"}},
		fakeLimiter{allow: true},
		fakeLimiter{allow: true},
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRouter_GlobalRateLimitBlocks(t *testing.T) {
	router := httpapi.NewRouter(fakeLocator{}, fakeLimiter{allow: false}, fakeLimiter{allow: true})

	req := httptest.NewRequest(http.MethodGet, "/v1/find-country?ip=8.8.8.8", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
```

This reuses `fakeLocator` and `fakeLimiter` already defined in `handlers_test.go` / `middleware_test.go` — same package, no redeclaration needed.

- [ ] **Step 11: Run test, verify it fails**

Run: `go test ./internal/httpapi/...`
Expected: build failure — `undefined: httpapi.NewRouter`

- [ ] **Step 12: Write minimal implementation**

Create `internal/httpapi/router.go`:

```go
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"go-ip2country/internal/geo"
	"go-ip2country/internal/ratelimit"
)

func NewRouter(locator geo.Locator, globalLimiter, perIPLimiter ratelimit.Limiter) http.Handler {
	r := chi.NewRouter()
	r.Use(RateLimit(globalLimiter, GlobalKey))
	r.Use(RateLimit(perIPLimiter, PerIPKey))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/v1/find-country", FindCountry(locator))

	return r
}
```

- [ ] **Step 13: Run test, verify it passes**

Run: `go test ./internal/httpapi/...`
Expected: PASS (all test functions across the three files)

- [ ] **Step 14: Commit**

```bash
git add go.mod go.sum internal/httpapi
git commit -m "feat: add HTTP handlers, rate-limit middleware, and router"
```

---

### Task 6: End-to-end integration test

**Files:**
- Test: `internal/httpapi/integration_test.go`

**Interfaces:**
- Consumes: `httpapi.NewRouter` (Task 5), `geocsv.New` (Task 4), `ratelimit.NewFixedWindow` (Task 2), `config.Config` (Task 1)

This test wires the *real* CSV locator (against the repo-root `testdata/ip2country.csv` fixture) and *real* `FixedWindow` limiters behind the real router — no fakes. It doesn't drive new production code (everything it needs already exists from Tasks 1–5), so it isn't expected to fail first; it exists as a regression guard over the fully-wired stack, matching the design doc's testing strategy.

- [ ] **Step 1: Write the test**

Create `internal/httpapi/integration_test.go`:

```go
package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
	geocsv "go-ip2country/internal/geo/csv"
	"go-ip2country/internal/httpapi"
	"go-ip2country/internal/ratelimit"
)

func TestEndToEnd_FindCountryAndRateLimit(t *testing.T) {
	locator, err := geocsv.New(config.Config{DatastoreCSVPath: "../../testdata/ip2country.csv"})
	require.NoError(t, err)

	perIPLimiter := ratelimit.NewFixedWindow(1, time.Second)
	defer perIPLimiter.Close()
	globalLimiter := ratelimit.NewFixedWindow(100, time.Second)
	defer globalLimiter.Close()

	router := httpapi.NewRouter(locator, globalLimiter, perIPLimiter)
	server := httptest.NewServer(router)
	defer server.Close()

	resp1, err := http.Get(server.URL + "/v1/find-country?ip=2.22.233.255")
	require.NoError(t, err)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	assert.Equal(t, `{"country":"GBR","city":"London"}`+"\n", string(body1))

	resp2, err := http.Get(server.URL + "/v1/find-country?ip=2.22.233.255")
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusTooManyRequests, resp2.StatusCode, "second request within the same second should be rate limited")
}
```

- [ ] **Step 2: Run test, verify it passes**

Run: `go test ./internal/httpapi/... -run TestEndToEnd -v`
Expected: PASS. If it fails, the bug is in the wiring between already-tested units (e.g. middleware order in `NewRouter`, or a key-extraction bug in `PerIPKey` given `httptest.Server`'s loopback `RemoteAddr`) — fix the relevant Task 2/5 file and rerun.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./...`
Expected: PASS across every package written so far.

- [ ] **Step 4: Commit**

```bash
git add internal/httpapi/integration_test.go
git commit -m "test: add end-to-end test covering real datastore and rate limiter wiring"
```

---

### Task 7: `main.go` wiring, graceful shutdown, manual verification

**Files:**
- Create: `cmd/server/main.go`
- Test: `cmd/server/main_test.go` (covers the one pure function, `parseLevel`)

**Interfaces:**
- Consumes: everything from Tasks 1–5 (`config.Load`, `geo.New`, the blank import `go-ip2country/internal/geo/csv` to trigger its `init()` registration, `ratelimit.NewFixedWindow`, `httpapi.NewRouter`)

- [ ] **Step 1: Write the failing test for `parseLevel`**

Create `cmd/server/main_test.go`:

```go
package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLevel(t *testing.T) {
	assert.Equal(t, slog.LevelDebug, parseLevel("debug"))
	assert.Equal(t, slog.LevelWarn, parseLevel("warn"))
	assert.Equal(t, slog.LevelError, parseLevel("error"))
	assert.Equal(t, slog.LevelInfo, parseLevel("info"))
	assert.Equal(t, slog.LevelInfo, parseLevel("anything-else"))
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./cmd/server/...`
Expected: build failure — `undefined: parseLevel` (package `main` has no files yet)

- [ ] **Step 3: Write `main.go`**

Create `cmd/server/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
	_ "go-ip2country/internal/geo/csv"
	"go-ip2country/internal/httpapi"
	"go-ip2country/internal/ratelimit"
)

func main() {
	logLevel := new(slog.LevelVar)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logLevel.Set(parseLevel(cfg.LogLevel))

	locator, err := geo.New(cfg.DatastoreType, cfg)
	if err != nil {
		logger.Error("failed to initialize datastore", "error", err)
		os.Exit(1)
	}

	globalLimiter := ratelimit.NewFixedWindow(cfg.RateLimitGlobalRPS, time.Second)
	perIPLimiter := ratelimit.NewFixedWindow(cfg.RateLimitPerIPRPS, time.Second)
	defer globalLimiter.Close()
	defer perIPLimiter.Close()

	router := httpapi.NewRouter(locator, globalLimiter, perIPLimiter)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("starting server", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
```

`main()` itself is orchestration/wiring with no independent logic beyond `parseLevel` (already tested) — it's verified manually below rather than unit tested, which is standard practice for Go `main` functions.

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./cmd/server/...`
Expected: PASS

- [ ] **Step 5: Build the binary**

Run: `go build -o /tmp/go-ip2country ./cmd/server`
Expected: builds with no errors.

- [ ] **Step 6: Manually run and verify the server**

Run (in the background or a separate terminal):

```bash
DATASTORE_TYPE=csv \
DATASTORE_CSV_PATH=testdata/ip2country.csv \
RATE_LIMIT_GLOBAL_RPS=100 \
RATE_LIMIT_PER_IP_RPS=2 \
SERVER_PORT=8080 \
/tmp/go-ip2country
```

In another terminal, verify each case:

```bash
curl -s -o /dev/null -w '%{http_code}\n' 'http://localhost:8080/healthz'
# expect 200

curl -s 'http://localhost:8080/v1/find-country?ip=2.22.233.255'
# expect {"country":"GBR","city":"London"}

curl -s -w '\n%{http_code}\n' 'http://localhost:8080/v1/find-country'
# expect {"error":"missing required query parameter: ip"} and 400

curl -s -w '\n%{http_code}\n' 'http://localhost:8080/v1/find-country?ip=1.2.3.4'
# expect {"error":"location not found for ip"} and 404

for i in 1 2 3; do curl -s -o /dev/null -w '%{http_code}\n' 'http://localhost:8080/v1/find-country?ip=2.22.233.255'; done
# expect 200, then 429s (per-IP limit is 2/sec)
```

Then send `SIGTERM` (e.g. `kill <pid>` or Ctrl-C) and confirm the log shows `"shutting down"` followed by clean process exit (no panic, no forced-kill needed).

- [ ] **Step 7: Commit**

```bash
git add cmd/server
git commit -m "feat: wire config, datastore, rate limiters, and router into a runnable server"
```

---

### Task 8: Dockerfile

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

- [ ] **Step 1: Write `.dockerignore`**

Create `.dockerignore`:

```
.git
docs
*.md
```

- [ ] **Step 2: Write the Dockerfile**

Create `Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/go-ip2country ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=builder /out/go-ip2country /usr/local/bin/go-ip2country
COPY --from=builder /src/testdata/ip2country.csv /data/ip2country.csv
ENV SERVER_PORT=8080 \
    DATASTORE_TYPE=csv \
    DATASTORE_CSV_PATH=/data/ip2country.csv \
    RATE_LIMIT_GLOBAL_RPS=100 \
    RATE_LIMIT_PER_IP_RPS=10 \
    LOG_LEVEL=info
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/go-ip2country"]
```

- [ ] **Step 3: Start the Docker runtime**

This machine uses Colima as the Docker backend, and it isn't running by default.

Run: `colima start`
Expected: Colima starts; `docker info` succeeds afterward.

- [ ] **Step 4: Build the image**

Run: `docker build -t go-ip2country:local .`
Expected: builds successfully through both stages.

- [ ] **Step 5: Run the container and verify it**

```bash
docker run --rm -d -p 8080:8080 --name go-ip2country-test go-ip2country:local
sleep 1
curl -s 'http://localhost:8080/v1/find-country?ip=2.22.233.255'
# expect {"country":"GBR","city":"London"}
docker stop go-ip2country-test
```

- [ ] **Step 6: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "build: add multi-stage Dockerfile"
```

---

### Task 9: README and final verification

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write the README**

Create `README.md`:

```markdown
# go-ip2country

A REST service that resolves an IP address to a country and city, with a
hand-rolled global + per-IP rate limiter and a pluggable datastore layer.

## Run

```bash
export DATASTORE_TYPE=csv
export DATASTORE_CSV_PATH=testdata/ip2country.csv
export RATE_LIMIT_GLOBAL_RPS=100
export RATE_LIMIT_PER_IP_RPS=10
export SERVER_PORT=8080

go run ./cmd/server
```

```bash
curl 'http://localhost:8080/v1/find-country?ip=2.22.233.255'
# {"country":"GBR","city":"London"}
```

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `SERVER_PORT` | no | `8080` | TCP port the HTTP server listens on. |
| `DATASTORE_TYPE` | no | `csv` | Selects the datastore implementation. |
| `DATASTORE_CSV_PATH` | yes, when `DATASTORE_TYPE=csv` | — | Path to the CSV dataset (`ip,city,country` per line). |
| `RATE_LIMIT_GLOBAL_RPS` | yes | — | Global requests/sec budget, shared across all clients. |
| `RATE_LIMIT_PER_IP_RPS` | yes | — | Requests/sec budget per client IP. |
| `LOG_LEVEL` | no | `info` | One of `debug`, `info`, `warn`, `error`. |

## API

`GET /v1/find-country?ip=<ip>` → `200 {"country":"XXX","city":"xxx"}`

Errors → `{"error":"..."}` with `400` (missing/invalid `ip`), `404` (no match),
`429` (rate limit exceeded), or `500`.

`GET /healthz` → `200` (liveness check, not rate limited).

## Extending the datastore

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
go test ./...
```

## Docker

```bash
docker build -t go-ip2country:local .
docker run --rm -p 8080:8080 \
  -e RATE_LIMIT_GLOBAL_RPS=100 -e RATE_LIMIT_PER_IP_RPS=10 \
  go-ip2country:local
```
```

- [ ] **Step 2: Run full verification suite**

```bash
gofmt -l .
```
Expected: no output (nothing unformatted). If any files are listed, run `gofmt -w .` and re-check.

```bash
go vet ./...
```
Expected: no output.

```bash
go test ./... -v
```
Expected: PASS for every package.

```bash
go mod tidy
```
Expected: `go.mod`/`go.sum` unchanged (or only whitespace-equivalent) — confirms no stray/missing dependencies.

- [ ] **Step 3: Commit**

```bash
git add README.md go.mod go.sum
git commit -m "docs: add README with usage, config, and extension instructions"
```
