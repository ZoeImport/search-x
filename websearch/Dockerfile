FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.work go.work.sum ./
COPY runtime/go.mod runtime/go.sum runtime/
COPY websearch/go.mod websearch/go.sum websearch/
RUN cd websearch && go mod download
COPY runtime runtime
COPY websearch websearch
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/websearch-api ./websearch/cmd/websearch-api

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates chromium fonts-noto-cjk tini && apt-get clean && \
    useradd --create-home --uid 10001 websearch && mkdir -p /data/profiles /data/manifests /data/debug /var/log/websearch-api && chown -R websearch:websearch /data /var/log/websearch-api
COPY --from=builder /out/websearch-api /usr/local/bin/websearch-api
COPY websearch/config.yaml /etc/websearch/config.yaml
USER websearch
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/websearch-api", "-config", "/etc/websearch/config.yaml"]
