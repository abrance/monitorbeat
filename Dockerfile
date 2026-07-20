# Multi-stage build for monitorbeat
# Stage 1: build
FROM golang:1.25-alpine AS builder

ARG GOPROXY=https://goproxy.cn|https://goproxy.io|https://proxy.golang.org|direct
ENV GOPROXY=${GOPROXY}

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X main.version=release" \
    -o /monitorbeat ./cmd/monitorbeat

# Stage 2: runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /monitorbeat /usr/local/bin/monitorbeat

ENTRYPOINT ["/usr/local/bin/monitorbeat"]
CMD ["-config", "/etc/monitorbeat/config.yaml"]
