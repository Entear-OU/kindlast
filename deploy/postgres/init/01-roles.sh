#!/bin/bash
# The Postgres role split (ENT-192, core-api-surface §14.1).
#
# THIS RUNS BEFORE ANYTHING ELSE TOUCHES THE DATABASE. A plain Postgres
# container starts with a POSTGRES_USER that is a superuser, and superusers
# bypass row level security entirely. Even a non-superuser bypasses RLS on
# tables it owns unless the table sets FORCE ROW LEVEL SECURITY. Supabase
# shielded the app from this by running it as a dedicated non-superuser role;
# a self-managed container has to build that shield here.
#
# Three roles, three jobs, no overlap:
#
#   kindlast_migrator   owns the kindlast database and schema, runs the goose
#                       migrations and seeds. BYPASSRLS, because it is the
#                       maintenance role and never serves a request. The
#                       application never connects as it.
#
#   kindlast_app        the only role the application connects as.
#                       NOSUPERUSER, NOBYPASSRLS, owns nothing, holds
#                       table-level DML grants only (granted by the baseline
#                       migration). Row level security is its whole world.
#
#   kindlast_vector_ro  read-only role for Intelligence's vector search,
#                       scoped to the chunk/embedding tables once those land
#                       (ENT-51). Created now so the connection string exists
#                       from day one; it holds no grants yet.
#
# Passwords come from the environment (see deploy/compose.yaml). The defaults
# there are for local development only.
set -euo pipefail

: "${KINDLAST_MIGRATOR_PASSWORD:?KINDLAST_MIGRATOR_PASSWORD must be set}"
: "${KINDLAST_APP_PASSWORD:?KINDLAST_APP_PASSWORD must be set}"
: "${KINDLAST_VECTOR_RO_PASSWORD:?KINDLAST_VECTOR_RO_PASSWORD must be set}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
	create role kindlast_migrator
	  login password '${KINDLAST_MIGRATOR_PASSWORD}'
	  nosuperuser nocreatedb nocreaterole noinherit bypassrls;

	create role kindlast_app
	  login password '${KINDLAST_APP_PASSWORD}'
	  nosuperuser nocreatedb nocreaterole noinherit nobypassrls;

	create role kindlast_vector_ro
	  login password '${KINDLAST_VECTOR_RO_PASSWORD}'
	  nosuperuser nocreatedb nocreaterole noinherit nobypassrls;
EOSQL

# pgvector's extension is not marked trusted, so a non-superuser database
# owner cannot CREATE EXTENSION vector. Install it (and pgcrypto) into
# template1 as the superuser, before any application database exists: every
# database created from here on inherits both, including the scratch
# databases the db test suite creates.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname template1 <<-EOSQL
	create extension if not exists vector;
	create extension if not exists pgcrypto;
EOSQL

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
	create database kindlast owner kindlast_migrator;
	revoke all on database kindlast from public;
	grant connect on database kindlast
	  to kindlast_migrator, kindlast_app, kindlast_vector_ro;
EOSQL

# Schema ownership and the CREATE fence. The migrator owns public; the app
# and the vector reader may look but not build. Table-level grants are the
# baseline migration's job, so they are versioned with the schema they cover.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname kindlast <<-EOSQL
	alter schema public owner to kindlast_migrator;
	revoke create on schema public from public;
	grant usage on schema public to kindlast_app, kindlast_vector_ro;
EOSQL
