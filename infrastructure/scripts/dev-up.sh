#!/bin/bash
set -e

echo "Starting Kindlast local dev environment..."

# Copy env file if it doesn't exist
if [ ! -f .env.local ]; then
  if [ -f .env.example ]; then
    cp .env.example .env.local
    echo "Created .env.local from .env.example"
  else
    echo "Warning: No .env.example found. Create .env.local manually."
  fi
fi

# Start database services first
echo "Starting databases..."
docker compose up -d qdrant redis postgres

# Wait for databases to be healthy
echo "Waiting for databases to be ready..."
sleep 5

# Check if services are healthy
echo "Checking database health..."
docker compose ps

# Seed Qdrant collections
echo "Seeding Qdrant collections..."
bash "$(dirname "$0")/seed-qdrant.sh"

# Start application services
echo "Starting application services..."
docker compose up -d gateway rag frontend

echo ""
echo "Kindlast is ready!"
echo "  Frontend:  http://localhost:3000"
echo "  Gateway:   http://localhost:8080"
echo "  RAG:       http://localhost:8081"
echo "  Qdrant:    http://localhost:6333"
echo "  Postgres:  localhost:5432"
echo "  Redis:     localhost:6379"
echo ""
echo "To start ingestion manually:"
echo "  docker compose --profile ingestion up ingestion"
