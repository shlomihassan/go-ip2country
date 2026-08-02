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
