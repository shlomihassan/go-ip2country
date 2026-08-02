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
