# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

# Allow the Go toolchain to auto-download the version required by go.mod
ENV GOTOOLCHAIN=auto

WORKDIR /app

# Download dependencies first (layer-cached unless go.mod/go.sum change)
COPY go.mod go.sum ./
RUN go mod download

# Copy source (web/static is embedded at compile time via go:embed)
COPY . .

# Build a fully static binary — no libc, no cgo
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
    go build -trimpath -ldflags="-s -w" \
    -o /civic-sync-server ./cmd/server

# ── Stage 2: Minimal runtime ──────────────────────────────────────────────────
# distroless/static includes CA certificates (needed for HTTPS to Google APIs)
# while remaining minimal — no shell, no package manager.
FROM gcr.io/distroless/static:nonroot

# Copy the static binary (web/static assets are embedded inside it)
COPY --from=builder /civic-sync-server /civic-sync-server

EXPOSE 8080

ENTRYPOINT ["/civic-sync-server"]
