# RAG Service Dockerfile
# Go RAG service with provider abstraction, hybrid search, and streaming

FROM golang:1.24-alpine AS builder

# Install git for version info and ca-certificates for HTTPS
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go.mod and go.sum first for better layer caching
COPY services/rag/go.mod services/rag/go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY services/rag/ .

# Build the binary with security flags
# -w: Omit DWARF symbol table
# -s: Omit symbol table and debug information
# -X: Inject version at build time
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -X main.version=${VERSION}" \
    -o rag ./cmd/server

# Final stage: scratch for minimal attack surface
FROM scratch

# Copy CA certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data for proper time handling
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary
COPY --from=builder /app/rag /rag

# Expose the RAG service port
EXPOSE 8081

# Run as non-root user (UID 1001)
USER 1001

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/rag", "healthcheck"] || exit 1

ENTRYPOINT ["/rag"]
