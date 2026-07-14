FROM golang:1.26-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/search-api ./cmd/server

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates chromium curl fonts-noto-cjk tini \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 search \
    && mkdir -p /data/chrome-profile /data/bing-profile /data/debug \
    && chown -R search:search /data

COPY --from=builder /out/search-api /usr/local/bin/search-api

USER search
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/search-api"]
