FROM golang:1.26-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/writeups-mcp .

FROM alpine:latest

RUN apk add --no-cache ca-certificates curl

WORKDIR /app

COPY --from=builder /out/writeups-mcp /usr/local/bin/writeups-mcp

EXPOSE 9001

VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:9001/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/writeups-mcp"]
CMD ["-transport", "http", "-host", "0.0.0.0", "-port", "9001"]
