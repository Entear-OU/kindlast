#!/bin/bash
# postgres-platform bootstrap (ENT-192, core-api-surface §14.3).
#
# This container holds infrastructure state whose schemas we do not own:
# Zitadel, and Temporal's two logical databases (temporal,
# temporal_visibility). Nobody writes migrations for anything here: Zitadel
# self-migrates on boot and Temporal ships temporal-sql-tool. Each product gets
# its own database and its own role so neither can read the other, and the
# application NEVER connects to this container at all (four roles, four
# connection strings, no overlap with postgres-app).
#
# ONLY ZITADEL IS PROVISIONED HERE, AND THAT IS NOT AN OVERSIGHT. This
# directory runs once, when the volume is first initialised. Temporal arrived
# (ENT-256) after every existing volume had already been initialised, so a
# script for it here would run on a fresh checkout and on nothing else. Its
# role and databases are created by the `temporal-init` job in
# deploy/temporal/init.sh instead, which runs on every boot and does nothing
# when there is nothing to do. Anything else that joins this server later
# should take that shape too, for the same reason.
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
