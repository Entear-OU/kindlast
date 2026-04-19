#!/bin/bash
#
# Kindlast Local Development Shutdown Script
# Stops all services started by dev-up.sh.
#
# Usage:
#   ./scripts/dev-down.sh        # Stop services, keep volumes
#   ./scripts/dev-down.sh -v     # Stop services and remove volumes
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "Kindlast Development Environment Shutdown"
echo "=========================================="
echo ""

# Check for -v flag to remove volumes
REMOVE_VOLUMES=""
if [ "$1" = "-v" ] || [ "$1" = "--volumes" ]; then
    REMOVE_VOLUMES="-v"
    echo "[WARN] Volumes will be removed. All data will be lost!"
    echo ""
fi

# Stop all services
echo "Stopping all services..."
if [ -n "$REMOVE_VOLUMES" ]; then
    docker compose --profile processors down -v
else
    docker compose --profile processors down
fi

echo ""
echo "=========================================="
if [ -n "$REMOVE_VOLUMES" ]; then
    echo "All services stopped and volumes removed."
else
    echo "All services stopped. Data volumes preserved."
    echo ""
    echo "To also remove volumes, run: ./scripts/dev-down.sh -v"
fi
