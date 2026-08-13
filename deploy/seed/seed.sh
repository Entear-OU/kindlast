#!/bin/sh
# Seed job (ENT-192): dev/test fixtures for the local stack.
#
#   1. postgres-app: two test organisations with members and subscriptions
#      (deploy/seed/seed.sql, as kindlast_migrator, idempotent).
#   2. Zitadel: the `kindlast` project, its role set (the §1.3 scope set of
#      the core-api-surface doc), and the `web` OIDC client (authorization
#      code + PKCE, confidential). The client id and generated secret are
#      written to /machinekey/web-client.json for the web app to pick up in
#      dev. Idempotent: existing objects are left alone.
#
# Runs on alpine with tools installed at start; this is a dev/CI job, not a
# production image.
set -eu

apk add --no-cache --quiet postgresql-client curl jq

echo "seed: applying postgres-app fixtures"
psql -v ON_ERROR_STOP=1 -q -f /seed.sql

PAT="$(tr -d '[:space:]' < /machinekey/seed-bot-pat.txt)"
AUTH="Authorization: Bearer ${PAT}"
HOST="Host: ${ZITADEL_HOST_HEADER}"

api() {
  method="$1"; path="$2"; body="${3:-}"
  if [ -n "$body" ]; then
    curl -sf -X "$method" -H "$AUTH" -H "$HOST" -H 'Content-Type: application/json' \
      -d "$body" "${ZITADEL_URL}${path}"
  else
    curl -sf -X "$method" -H "$AUTH" -H "$HOST" "${ZITADEL_URL}${path}"
  fi
}

echo "seed: ensuring zitadel project 'kindlast'"
PROJECT_ID="$(api POST /management/v1/projects/_search \
  '{"queries":[{"nameQuery":{"name":"kindlast","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
  | jq -r '.result[0].id // empty')"

if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID="$(api POST /management/v1/projects '{"name":"kindlast"}' | jq -r '.id')"
  echo "seed: created project ${PROJECT_ID}"
else
  echo "seed: project exists (${PROJECT_ID})"
fi

# The scope set (core-api-surface §1.3) as project roles. records:write is
# already split per resource per §23.3. Machine scopes (internal:*) are
# registered too: they are issued via client_credentials to service clients
# at build-order step 1, never to web.
for role in \
  findings:read findings:act \
  records:read records:ropa:write records:dsar:write records:ai-systems:write \
  dashboard:read onboarding:write \
  notifications:read notifications:write \
  billing:read billing:manage \
  audit:read org:read org:manage \
  internal:ingest internal:intelligence internal:act-on-behalf
do
  api POST "/management/v1/projects/${PROJECT_ID}/roles" \
    "{\"roleKey\":\"${role}\",\"displayName\":\"${role}\"}" > /dev/null 2>&1 \
    && echo "seed: role ${role} created" \
    || echo "seed: role ${role} exists"
done

# The audience core-api accepts (ENT-195, core-api-surface §1.4).
#
# Measured against this stack rather than taken from the design: Zitadel puts
# the PROJECT id in `aud` when a token is requested with the project's reserved
# audience scope, so the value core-api verifies is that id, not the friendly
# `kindlast-core-api` the document names. The API application below exists so
# the project has an API-typed app at all; the audience value is still the
# project id.
#
# Written to the shared volume because it is generated here and core-api needs
# it at boot. Same mechanism as the web client's credentials below.
echo "seed: ensuring 'core-api' API application"
API_APP_ID="$(api POST "/management/v1/projects/${PROJECT_ID}/apps/_search" \
  '{"queries":[{"nameQuery":{"name":"core-api","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
  | jq -r '.result[0].id // empty')"

if [ -z "$API_APP_ID" ]; then
  api POST "/management/v1/projects/${PROJECT_ID}/apps/api" \
    '{"name":"core-api","authMethodType":"API_AUTH_METHOD_TYPE_BASIC"}' > /dev/null
  echo "seed: created the core-api API application"
else
  echo "seed: core-api API application exists (${API_APP_ID})"
fi

printf '%s' "${PROJECT_ID}" > /machinekey/core-api-audience.txt
echo "seed: core-api audience (${PROJECT_ID}) written to the shared volume"

# A service user for the client-credentials grant, which the Postman collection
# marks as "NO CLIENT YET, ENT-195". It exists so API work does not need a
# browser: the human sign-in flow is authorization code with PKCE and cannot be
# driven from a collection.
echo "seed: ensuring 'core-api-client' service user"
MACHINE_ID="$(api POST /management/v1/users/_search \
  '{"queries":[{"userNameQuery":{"userName":"core-api-client","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
  | jq -r '.result[0].id // empty')"

if [ -z "$MACHINE_ID" ]; then
  MACHINE_ID="$(api POST /management/v1/users/machine \
    '{"userName":"core-api-client","name":"Core API client","description":"client_credentials for local API work (ENT-195)","accessTokenType":"ACCESS_TOKEN_TYPE_JWT"}' \
    | jq -r '.userId')"

  # The secret response carries its own clientId, and it is the USERNAME rather
  # than the user id. Passing the user id as client_id returns
  # "invalid_client: client not found", which is a confusing way to spend
  # twenty minutes.
  api PUT "/management/v1/users/${MACHINE_ID}/secret" '{}' \
    | jq '{clientId, clientSecret}' > /machinekey/core-api-client.json

  echo "seed: created service user core-api-client (${MACHINE_ID})"
  echo "seed: credentials written to the zitadel-machinekey volume (core-api-client.json)"
else
  echo "seed: service user core-api-client exists (${MACHINE_ID})"
fi

echo "seed: ensuring 'web' OIDC client"
APP_ID="$(api POST "/management/v1/projects/${PROJECT_ID}/apps/_search" \
  '{"queries":[{"nameQuery":{"name":"web","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
  | jq -r '.result[0].id // empty')"

if [ -z "$APP_ID" ]; then
  RESPONSE="$(api POST "/management/v1/projects/${PROJECT_ID}/apps/oidc" "{
    \"name\": \"web\",
    \"redirectUris\": [\"${WEB_REDIRECT_URI}\"],
    \"postLogoutRedirectUris\": [\"http://localhost:3000/\"],
    \"responseTypes\": [\"OIDC_RESPONSE_TYPE_CODE\"],
    \"grantTypes\": [\"OIDC_GRANT_TYPE_AUTHORIZATION_CODE\", \"OIDC_GRANT_TYPE_REFRESH_TOKEN\"],
    \"appType\": \"OIDC_APP_TYPE_WEB\",
    \"authMethodType\": \"OIDC_AUTH_METHOD_TYPE_BASIC\",
    \"accessTokenType\": \"OIDC_TOKEN_TYPE_JWT\",
    \"devMode\": true
  }")"
  echo "$RESPONSE" | jq '{clientId, clientSecret}' > /machinekey/web-client.json
  echo "seed: created web client $(echo "$RESPONSE" | jq -r '.clientId')"
  echo "seed: credentials written to the zitadel-machinekey volume (web-client.json)"
else
  echo "seed: web client exists (${APP_ID})"
fi

echo "seed: done"
