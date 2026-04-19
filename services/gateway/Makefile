.PHONY: help build run test clean dev deps fmt vet lint

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Download dependencies
	go mod download
	go mod verify

build: ## Build the gateway binary
	go build -o bin/gateway cmd/server/main.go

run: ## Run the gateway service
	go run cmd/server/main.go

dev: deps ## Run in development mode with auto-reload (requires air)
	air

test: ## Run tests
	go test -v -race -cover ./...

test-coverage: ## Run tests with coverage report
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

fmt: ## Format code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

lint: fmt vet ## Run formatters and linters

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html
	go clean

docker-build: ## Build Docker image
	docker build -t kindlast/gateway:latest .

docker-run: ## Run Docker container
	docker run -p 8080:8080 --env-file .env kindlast/gateway:latest

.DEFAULT_GOAL := help
