# syntax=docker/dockerfile:1

FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath -ldflags="-s -w" -o /out/go-ip2country ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35
COPY --from=builder --chown=65532:65532 /out/go-ip2country /usr/local/bin/go-ip2country
COPY --from=builder --chown=65532:65532 /src/testdata/ip2country.csv /data/ip2country.csv
ENV SERVER_PORT=8080 \
    DATASTORE_TYPE=csv \
    DATASTORE_CSV_PATH=/data/ip2country.csv \
    RATE_LIMIT_GLOBAL_RPS=100 \
    RATE_LIMIT_PER_IP_RPS=10 \
    LOG_LEVEL=info
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/go-ip2country"]
