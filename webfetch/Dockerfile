FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.work go.work.sum ./
COPY runtime/go.mod runtime/go.sum runtime/
COPY webfetch/go.mod webfetch/go.sum webfetch/
RUN cd webfetch && go mod download
COPY runtime runtime
COPY webfetch webfetch
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/webfetch-api ./webfetch/cmd/webfetch-api

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates chromium fonts-noto-cjk tini && apt-get clean && \
    useradd --create-home --uid 10001 webfetch && mkdir -p /data/profile /var/log/webfetch-api && chown -R webfetch:webfetch /data /var/log/webfetch-api
COPY --from=builder /out/webfetch-api /usr/local/bin/webfetch-api
COPY webfetch/config.yaml /etc/webfetch/config.yaml
USER webfetch
EXPOSE 8081
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/webfetch-api", "-config", "/etc/webfetch/config.yaml"]
