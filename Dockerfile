# Build stage
FROM golang:1.26-alpine AS build

WORKDIR /src

# Download the dependencies first so that they are cached.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl

# Runtime stage
FROM alpine:3.21

# Certificates for outbound HTTPS and a timezone database.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app

WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/adminctl /app/adminctl

# The database lives here, mount a volume to keep it.
RUN mkdir -p /data && chown -R app:app /data /app
VOLUME ["/data"]

ENV ADDR=:8080 \
    DB_PATH=/data/app.db \
    COOKIE_SECURE=false \
    LOG_LEVEL=info

USER app
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/server"]
