#!/bin/bash
# postgres-platform bootstrap (ENT-192, core-api-surface §14.3).
#
# This container holds infrastructure state whose schemas we do not own:
# Zitadel now, Temporal's two logical databases (temporal,
# temporal_visibility) when build-order step 8 brings the workflow engine in.
# Nobody writes migrations for anything here: Zitadel self-migrates on boot
# and Temporal ships temporal-sql-tool. Each product gets its own database
# and its own role so neither can read the other, and the application NEVER
# connects to this container at all (four roles, four connection strings, no
# overlap with postgres-app).
set -euo pipefail

: "${ZITADEL_DB_PASSWORD:?ZITADEL_DB_PASSWORD must be set}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
	create role zitadel
	  login password '${ZITADEL_DB_PASSWORD}'
	  nosuperuser nocreatedb nocreaterole;

	create database zitadel owner zitadel;
	revoke all on database zitadel from public;
	grant connect on database zitadel to zitadel;
EOSQL
