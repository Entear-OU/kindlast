# Build stage
FROM golang:1.24-alpine3.21 AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with security hardening
RUN CGO_ENABLED=0 GOOS=linux go build \
    -a \
    -installsuffix cgo \
    -ldflags="-w -s -extldflags '-static'" \
    -trimpath \
    -o gateway \
    cmd/server/main.go

# Build healthcheck binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -a \
    -installsuffix cgo \
    -ldflags="-w -s -extldflags '-static'" \
    -trimpath \
    -o healthcheck \
    cmd/healthcheck/main.go

# Runtime stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

# Copy ca-certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary from builder
COPY --from=builder /app/gateway /gateway

# Copy healthcheck binary from builder
COPY --from=builder /app/healthcheck /healthcheck

# Use non-root user (distroless nonroot user)
USER nonroot:nonroot

# Expose port
EXPOSE 8080

# Run binary
ENTRYPOINT ["/gateway"]
