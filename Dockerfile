# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/lark-server ./cmd/lark-server

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

COPY --from=builder /app/bin/lark-server /app/

RUN mkdir -p /app/data

EXPOSE 4001

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:4001/health || exit 1

VOLUME ["/app/data"]

ENTRYPOINT ["/app/lark-server"]
