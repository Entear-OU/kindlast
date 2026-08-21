#!/bin/sh
# Provisions Temporal's role and its two databases on postgres-platform
# (ENT-256, core-api-surface §16.3).
#
# A JOB THAT RUNS EVERY BOOT, NOT AN INIT SCRIPT, and the difference is the
# whole reason this file exists beside deploy/postgres/platform-init/ rather
# than inside it.
#
# docker-entrypoint-initdb.d runs exactly once, when the data directory is
# empty. That is fine for Zitadel, which has been there since the volume was
# first created on every stack that exists. Temporal is arriving later, onto
# volumes that are already initialised, so an init script for it would run on
# a fresh checkout and on nothing else: every existing development stack and
# every self-hoster upgrading would bring `temporal` up against a database
# that has no role for it, and auto-setup would fail on its first connection.
# "It works on a fresh volume" is exactly the sort of fix that is not one, and
# this repository has already paid for that shape once (the web client's
# redirect URIs, ENT-241).
#
# So this is the `migrate` shape instead: a job container that runs on every
# `up`, does nothing when there is nothing to do, and must exit zero before
# `temporal` starts. Idempotent by checking before creating, because `create
# role` and `create database` have no `if not exists`, and a second `up` on an
# existing volume must not fail.
#
# WHY A ROLE OF ITS OWN
#
# Same rule as Zitadel and for the same reason: this container holds
# infrastructure state whose schemas we do not own, each product gets its own
# database and its own role so neither can read the other, and the application
# never connects here at all. Temporal runs `temporal-sql-tool` as this role to
# create and migrate its own schema, which is why the role owns the databases
# rather than merely connecting to them, and why nobody writes migrations for
# anything on this server.
#
# Two databases, not one. Temporal keeps workflow state (`temporal`) and its
# visibility store (`temporal_visibility`) apart by default, and the schema
# tool expects both. §16.3 says to skip Elasticsearch and let visibility run
# on the same Postgres until list queries make it slow, which is what this
# shape gives.
set -eu

: "${TEMPORAL_DB_PASSWORD:?TEMPORAL_DB_PASSWORD must be set}"

# PGHOST, PGUSER and PGPASSWORD arrive from compose: the platform superuser,
# which is the only role that can create roles and databases here. The
# application's credentials never appear in this container.
run() { psql -v ON_ERROR_STOP=1 --dbname postgres "$@"; }

if ! run -tAc "select 1 from pg_roles where rolname = 'temporal'" | grep -qx 1; then
  echo "temporal-init: creating role temporal"
  run -c "create role temporal login password '${TEMPORAL_DB_PASSWORD}' nosuperuser nocreatedb nocreaterole"
else
  echo "temporal-init: role temporal exists"
fi

for db in temporal temporal_visibility; do
  if ! run -tAc "select 1 from pg_database where datname = '${db}'" | grep -qx 1; then
    echo "temporal-init: creating database ${db}"
    run -c "create database ${db} owner temporal"
    run -c "revoke all on database ${db} from public"
    run -c "grant connect on database ${db} to temporal"
  else
    echo "temporal-init: database ${db} exists"
  fi
done

echo "temporal-init: done"
