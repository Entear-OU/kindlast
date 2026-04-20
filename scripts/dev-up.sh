#!/bin/bash
#
# Kindlast Local Development Startup Script
# Starts all services needed for local development.
#
# Usage:
#   ./scripts/dev-up.sh
#
# Prerequisites:
#   - Docker and Docker Compose installed
#   - .env.example in the repository root
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "Kindlast Development Environment"
echo "================================="
echo ""

# Step 1: Copy .env.example to .env.local if it doesn't exist
if [ ! -f .env.local ]; then
    if [ -f .env.example ]; then
        echo "[1/7] Creating .env.local from .env.example..."
        cp .env.example .env.local
        echo "      Created .env.local - please configure API keys before running services"
    else
        echo "[ERROR] .env.example not found. Cannot create .env.local"
        exit 1
    fi
else
    echo "[1/7] .env.local already exists, skipping copy"
fi

# Step 2: Start infrastructure services
echo ""
echo "[2/7] Starting infrastructure services (qdrant, redis, postgres)..."
docker compose up -d qdrant redis postgres

# Step 3: Wait for databases to be ready
echo ""
echo "[3/7] Waiting for databases to be ready..."

# Wait for health checks or timeout after 30 seconds
TIMEOUT=30
ELAPSED=0

echo "      Checking Qdrant..."
until curl -s http://localhost:6333/healthz > /dev/null 2>&1 || [ $ELAPSED -ge $TIMEOUT ]; do
    sleep 1
    ELAPSED=$((ELAPSED + 1))
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "      [WARN] Qdrant health check timed out, continuing anyway..."
else
    echo "      Qdrant is ready"
fi

echo "      Checking Redis..."
ELAPSED=0
until docker compose exec -T redis redis-cli ping > /dev/null 2>&1 || [ $ELAPSED -ge $TIMEOUT ]; do
    sleep 1
    ELAPSED=$((ELAPSED + 1))
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "      [WARN] Redis health check timed out, continuing anyway..."
else
    echo "      Redis is ready"
fi

echo "      Checking PostgreSQL..."
ELAPSED=0
until docker compose exec -T postgres pg_isready -U kindlast > /dev/null 2>&1 || [ $ELAPSED -ge $TIMEOUT ]; do
    sleep 1
    ELAPSED=$((ELAPSED + 1))
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "      [WARN] PostgreSQL health check timed out, continuing anyway..."
else
    echo "      PostgreSQL is ready"
fi

# Step 4: Run database migrations
echo ""
echo "[4/7] Running database migrations..."
bash "${SCRIPT_DIR}/run-migrations.sh"

# Step 5: Seed Qdrant collections
echo ""
echo "[5/7] Creating Qdrant collections..."
QDRANT_HOST=localhost:6333 bash "${SCRIPT_DIR}/seed-qdrant.sh"

# Step 6: Seed processor profiles
echo ""
echo "[6/7] Seeding processor profiles..."
docker compose --profile processors run --rm processor-ingestion || {
    echo "      [WARN] Processor ingestion failed or service not available"
    echo "      This is expected if the ingestion service is not yet built"
}

# Step 7: Start application services
echo ""
echo "[7/7] Starting application services (gateway, rag, frontend)..."
docker compose up -d gateway rag frontend

echo ""
echo "================================="
echo "Kindlast is starting up!"
echo ""
echo "Services:"
echo "  - Frontend:  http://localhost:3000"
echo "  - Gateway:   http://localhost:8080"
echo "  - RAG:       http://localhost:8081"
echo "  - Qdrant:    http://localhost:6333"
echo "  - Redis:     localhost:6379"
echo "  - Postgres:  localhost:5432"
echo ""
echo "Ready at http://localhost:3000"
echo ""
echo "Use 'docker compose logs -f' to view logs"
echo "Use './scripts/dev-down.sh' to stop all services"
