#!/bin/bash
#
# Database Migration Runner
# Runs all pending SQL migrations in order.
#
# Usage:
#   ./scripts/run-migrations.sh
#
# The script:
#   - Creates a schema_migrations table to track executed migrations
#   - Finds all .sql files in services/gateway/migrations/
#   - Runs any migrations that haven't been executed yet
#   - Records each successful migration
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MIGRATIONS_DIR="${PROJECT_ROOT}/services/gateway/migrations"

# Database credentials
DB_USER="${DB_USER:-kindlast}"
DB_NAME="${DB_NAME:-kindlast}"

# Run SQL command via docker compose exec
run_sql() {
    docker compose exec -T postgres psql -U "${DB_USER}" -d "${DB_NAME}" -q -t -c "$1"
}

# Run SQL file via docker compose exec (pipe file content)
# Use ON_ERROR_STOP to fail on first error
run_sql_file() {
    cat "$1" | docker compose exec -T postgres psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1
}

echo "Running database migrations..."

# Create schema_migrations table if it doesn't exist
run_sql "
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    executed_at TIMESTAMP NOT NULL DEFAULT NOW()
);
"

# Check if migrations directory exists
if [ ! -d "${MIGRATIONS_DIR}" ]; then
    echo "  No migrations directory found at ${MIGRATIONS_DIR}"
    exit 0
fi

# Get list of migration files sorted by name
MIGRATION_FILES=$(find "${MIGRATIONS_DIR}" -name "*.sql" -type f | sort)

if [ -z "${MIGRATION_FILES}" ]; then
    echo "  No migration files found"
    exit 0
fi

MIGRATIONS_RUN=0

for MIGRATION_FILE in ${MIGRATION_FILES}; do
    # Extract just the filename (e.g., "001_create_users_table.sql")
    MIGRATION_NAME=$(basename "${MIGRATION_FILE}")

    # Check if this migration has already been executed
    ALREADY_RUN=$(run_sql "SELECT COUNT(*) FROM schema_migrations WHERE version = '${MIGRATION_NAME}';" | tr -d ' ')

    if [ "${ALREADY_RUN}" -eq 0 ]; then
        echo "  Running: ${MIGRATION_NAME}"

        # Run the migration
        if run_sql_file "${MIGRATION_FILE}"; then
            # Record the migration
            run_sql "INSERT INTO schema_migrations (version) VALUES ('${MIGRATION_NAME}');"
            MIGRATIONS_RUN=$((MIGRATIONS_RUN + 1))
            echo "  Completed: ${MIGRATION_NAME}"
        else
            echo "  [ERROR] Failed to run migration: ${MIGRATION_NAME}"
            exit 1
        fi
    else
        echo "  Skipping (already run): ${MIGRATION_NAME}"
    fi
done

if [ ${MIGRATIONS_RUN} -eq 0 ]; then
    echo "  All migrations are up to date"
else
    echo "  Ran ${MIGRATIONS_RUN} migration(s)"
fi
