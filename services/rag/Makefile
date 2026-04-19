.PHONY: help build run test clean deps fmt lint vet

# Default target
help:
	@echo "RAG Service - Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  make build       - Build the service binary"
	@echo "  make run         - Run the service (requires .env file)"
	@echo "  make test        - Run tests"
	@echo "  make test-cover  - Run tests with coverage"
	@echo "  make deps        - Download dependencies"
	@echo "  make fmt         - Format code"
	@echo "  make lint        - Run linter (requires golangci-lint)"
	@echo "  make vet         - Run go vet"
	@echo "  make clean       - Remove built binaries"
	@echo ""

# Build the service
build:
	@echo "Building RAG service..."
	go build -o rag-service cmd/server/main.go
	@echo "Build complete: ./rag-service"

# Run the service
run:
	@echo "Starting RAG service..."
	@if [ ! -f .env ]; then echo "Warning: .env file not found. Copy .env.example to .env and configure."; fi
	go run cmd/server/main.go

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-cover:
	@echo "Running tests with coverage..."
	go test -cover -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install: https://golangci-lint.run/usage/install/"; exit 1)
	golangci-lint run ./...

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Clean built binaries
clean:
	@echo "Cleaning..."
	rm -f rag-service
	rm -f coverage.out coverage.html
	@echo "Clean complete"

# Development setup
dev-setup:
	@echo "Setting up development environment..."
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env from .env.example - please configure it"; fi
	@echo "Installing dependencies..."
	go mod download
	@echo "Setup complete! Edit .env file and run 'make run'"
