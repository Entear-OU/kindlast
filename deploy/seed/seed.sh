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
# Runs on the small image built by deploy/seed/Dockerfile, which carries psql,
# curl and jq. This script used to install them itself with `apk add`, on every
# single boot: a runtime fetch, and the one thing that stopped the stack coming
# up with egress blocked (ENT-240). The Dockerfile explains the move.
set -eu

echo "seed: applying postgres-app fixtures"
psql -v ON_ERROR_STOP=1 -q -f /seed.sql

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

# WAIT UNTIL THE MANAGEMENT API ANSWERS THIS CREDENTIAL, RATHER THAN ASSUMING
# IT DOES (ENT-240).
#
# `auth` reports healthy before Zitadel has finished setting the first instance
# up, so the seed bot's token can be absent from the shared volume, or present
# and not yet usable, at the moment this job starts. Nothing ever noticed,
# because the job used to open with `apk add curl jq postgresql-client` and
# those few seconds were doing the waiting.
#
# Taking that install out to make the stack run air-gapped removed the delay
# too, and the failure it exposed is the quiet kind. Every call 401s, `curl -sf`
# writes nothing, `jq` reads empty input and returns empty, and so every id
# below comes out blank: the seed reports "created project" with no id, writes
# an empty audience file, exits 0, and core-api then refuses to start because
# KINDLAST_OIDC_AUDIENCE is empty. A few seconds of package installation were
# the only thing between a working stack and that, which is a good argument for
# waiting on purpose instead.
PROJECT_QUERY='{"queries":[{"nameQuery":{"name":"kindlast","method":"TEXT_QUERY_METHOD_EQUALS"}}]}'
PROJECT_SEARCH=""
attempt=0
while [ "$attempt" -lt 60 ]; do
  # Re-read the token each time: on a cold volume the file appears late.
  PAT="$(tr -d '[:space:]' < /machinekey/seed-bot-pat.txt 2>/dev/null || true)"
  AUTH="Authorization: Bearer ${PAT}"
  if [ -n "$PAT" ] &&
    PROJECT_SEARCH="$(api POST /management/v1/projects/_search "$PROJECT_QUERY")"; then
    break
  fi
  PROJECT_SEARCH=""
  attempt=$((attempt + 1))
  sleep 1
done

if [ -z "$PROJECT_SEARCH" ]; then
  echo "seed: Zitadel never accepted the seed bot's token from" \
    "/machinekey/seed-bot-pat.txt (waited 60s)" >&2
  exit 1
fi

echo "seed: ensuring zitadel project 'kindlast'"
PROJECT_ID="$(printf '%s' "$PROJECT_SEARCH" | jq -r '.result[0].id // empty')"

if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID="$(api POST /management/v1/projects '{"name":"kindlast"}' | jq -r '.id // empty')"
  # Loud, because the alternative is a stack that comes up with an empty
  # audience and a core-api that will not start, three services away from here.
  if [ -z "$PROJECT_ID" ]; then
    echo "seed: creating the 'kindlast' project returned no id" >&2
    exit 1
  fi
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
  dashboard:read onboarding:read onboarding:write \
  notifications:read notifications:write \
  billing:read billing:manage \
  audit:read org:read org:manage \
  corpus:read \
  memory:read memory:write \
  model:read model:write \
  integrations:read integrations:write \
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

# The service user's authorization (ENT-221).
#
# Creating the roles is not granting them. Until this ran, core-api-client held
# a valid token carrying no roles at all, so every endpoint declaring a real
# scope answered permission_denied, and the sweep trigger the Postman
# collection documents could not be called by the credential that collection
# ships with.
#
# Machine grants are explicit and per-client on purpose. There are few of them,
# they are known at seed time, and least privilege is cheap to apply when the
# list is short: this client gets the internal set and nothing a human surface
# uses. Humans are the case that cannot be enumerated here, and they are what
# the rest of ENT-221 is about.
#
# Note for anyone testing this by hand: a grant alone is not enough. The caller
# must also request the reserved scope urn:zitadel:iam:org:projects:roles, or
# the roles never reach the token. Measured, and the plural is not a typo.
#
# Idempotent by search-then-create, like everything else here: a second `up` on
# an existing volume must not fail.
echo "seed: ensuring core-api-client holds the internal scopes"
MACHINE_GRANT="$(api POST "/management/v1/users/grants/_search" \
  "{\"queries\":[{\"userIdQuery\":{\"userId\":\"${MACHINE_ID}\"}}]}" \
  | jq -r '.result[0].id // empty')"

if [ -z "$MACHINE_GRANT" ]; then
  api POST "/management/v1/users/${MACHINE_ID}/grants" \
    "{\"projectId\":\"${PROJECT_ID}\",\"roleKeys\":[\"internal:ingest\",\"internal:intelligence\",\"internal:act-on-behalf\"]}" \
    > /dev/null 2>&1 \
    && echo "seed: granted internal:* to core-api-client" \
    || echo "seed: WARNING could not grant internal:* to core-api-client"
else
  echo "seed: core-api-client already holds a grant (${MACHINE_GRANT})"
fi

# TWO REDIRECT URIS, BECAUSE THERE ARE TWO CONSOLES (ENT-241).
#
# An authorization server refuses any redirect URI it was not given, so this
# list decides which consoles can complete a sign-in at all.
#
# WEB_REDIRECT_URI is the dev server on the host, which is what
# scripts/web-env.sh writes into .env.local and what `bun run test:e2e` drives
# by default. WEB_REDIRECT_URI_EDGE is the containerised console, which a
# browser reaches through the edge rather than on a port of its own.
#
# Both, rather than one, because both are real and they coexist: a maintainer
# runs the dev server against this stack while the stack serves the built
# console on the edge, and neither should take the other's sign-in away.
#
# `unique` because a deployment that sets them to the same value should get one
# entry rather than a duplicate.
REDIRECT_URIS="$(jq -cn --arg dev "$WEB_REDIRECT_URI" --arg edge "$WEB_REDIRECT_URI_EDGE" \
  '[$dev, $edge] | unique')"
# Where sign-out may return somebody: each console's own origin, derived from
# its callback rather than configured separately, so the two cannot drift.
LOGOUT_URIS="$(jq -cn --arg dev "$WEB_REDIRECT_URI" --arg edge "$WEB_REDIRECT_URI_EDGE" \
  '[$dev, $edge] | map(sub("auth/callback$"; "")) | unique')"

echo "seed: ensuring 'web' OIDC client"
APP_ID="$(api POST "/management/v1/projects/${PROJECT_ID}/apps/_search" \
  '{"queries":[{"nameQuery":{"name":"web","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
  | jq -r '.result[0].id // empty')"

if [ -z "$APP_ID" ]; then
  RESPONSE="$(api POST "/management/v1/projects/${PROJECT_ID}/apps/oidc" "{
    \"name\": \"web\",
    \"redirectUris\": ${REDIRECT_URIS},
    \"postLogoutRedirectUris\": ${LOGOUT_URIS},
    \"responseTypes\": [\"OIDC_RESPONSE_TYPE_CODE\"],
    \"grantTypes\": [\"OIDC_GRANT_TYPE_AUTHORIZATION_CODE\", \"OIDC_GRANT_TYPE_REFRESH_TOKEN\"],
    \"appType\": \"OIDC_APP_TYPE_WEB\",
    \"authMethodType\": \"OIDC_AUTH_METHOD_TYPE_BASIC\",
    \"accessTokenType\": \"OIDC_TOKEN_TYPE_JWT\",
    \"devMode\": true
  }")"
  echo "$RESPONSE" | jq '{clientId, clientSecret}' > /machinekey/web-client.json
  # The client id alone, for core-api (ENT-221).
  #
  # A separate file from web-client.json on purpose: that one holds the client
  # secret, and core-api has no business reading a credential it never uses.
  # This is the same shape as core-api-audience.txt, and for the same reason —
  # the value is generated here and cannot be baked into compose.
  echo "$RESPONSE" | jq -r '.clientId' > /machinekey/web-client-id.txt
  echo "seed: created web client $(echo "$RESPONSE" | jq -r '.clientId')"
  echo "seed: credentials written to the zitadel-machinekey volume (web-client.json)"
else
  # Re-published on every run, not only on creation: a stack whose volume
  # predates ENT-221 has the client but not the file, and core-api would then
  # silently fall back to granted scopes for humans.
  api POST "/management/v1/projects/${PROJECT_ID}/apps/_search" \
    '{"queries":[{"nameQuery":{"name":"web","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
    | jq -r '.result[0].oidcConfig.clientId // empty' > /machinekey/web-client-id.txt

  # The redirect URIs are re-published too, and this branch is the one that
  # matters most for ENT-241.
  #
  # A stack that is already up has a `web` client registered before the
  # containerised console existed, so its only redirect URI is the dev
  # server's. Creating the client is the path a clean checkout takes; every
  # existing stack takes this one, and without it the console would be served
  # and its sign-in refused with a redirect_uri mismatch. "It works on a fresh
  # volume" is exactly the sort of fix that is not one.
  #
  # This replaces the OIDC configuration rather than patching it, because the
  # API offers no patch, so every field the creation branch sets is repeated
  # here. It does not touch the client secret, which has its own endpoint, so a
  # re-run does not invalidate what web-client.json holds.
  api PUT "/management/v1/projects/${PROJECT_ID}/apps/${APP_ID}/oidc_config" "{
    \"redirectUris\": ${REDIRECT_URIS},
    \"postLogoutRedirectUris\": ${LOGOUT_URIS},
    \"responseTypes\": [\"OIDC_RESPONSE_TYPE_CODE\"],
    \"grantTypes\": [\"OIDC_GRANT_TYPE_AUTHORIZATION_CODE\", \"OIDC_GRANT_TYPE_REFRESH_TOKEN\"],
    \"appType\": \"OIDC_APP_TYPE_WEB\",
    \"authMethodType\": \"OIDC_AUTH_METHOD_TYPE_BASIC\",
    \"accessTokenType\": \"OIDC_TOKEN_TYPE_JWT\",
    \"devMode\": true
  }" > /dev/null \
    && echo "seed: web client redirect URIs published ${REDIRECT_URIS}" \
    || echo "seed: WARNING could not publish the web client redirect URIs"

  echo "seed: web client exists (${APP_ID})"
fi

# Mail, so that registration and verification actually complete on this stack.
#
# Without this, everything looks configured and nothing is delivered: Zitadel
# accepts the sign-up form, tries to send a verification code, and fails. The
# person is left with an account they cannot activate, and the only sign is a
# line in the auth container's log.
#
# Two things here cost an afternoon each and neither is guessable.
#
# **Zitadel will not use an SMTP provider that has no credentials**, and the
# error it reports for one is `Errors.SMTPConfig.NotFound`. That is not a
# missing config: `ListSMTPConfigs` returns it, the projection row is present
# with state active, and the notifier still refuses it, which sends you looking
# for a config that was there all along (zitadel/zitadel#8344). So a username
# and password are set even though Mailpit wants neither, and that is why
# compose runs Mailpit with `MP_SMTP_AUTH_ACCEPT_ANY` and
# `MP_SMTP_AUTH_ALLOW_INSECURE`: any credentials are accepted, so these values
# are arbitrary and development-only.
#
# **Creating a provider does not enable it.** It has to be activated in a
# second call, and a provider that exists but is inactive fails exactly the
# same way as one that does not exist.
echo "seed: ensuring the mail provider"
# `.result // []`, because a search that matches nothing omits the key
# entirely rather than returning an empty array, and iterating null is a jq
# error printed on the one path that is completely normal: the first boot.
SMTP_ID="$(api POST /admin/v1/smtp/_search '{}' \
  | jq -r '(.result // [])[] | select(.description == "Mailpit, the local development mailbox") | .id' \
  | head -n 1)"

if [ -z "$SMTP_ID" ]; then
  SMTP_ID="$(api POST /admin/v1/smtp "{
    \"senderAddress\": \"${SMTP_SENDER_ADDRESS}\",
    \"senderName\": \"${SMTP_SENDER_NAME}\",
    \"host\": \"${SMTP_HOST}\",
    \"tls\": false,
    \"description\": \"Mailpit, the local development mailbox\",
    \"user\": \"${SMTP_USER}\",
    \"password\": \"${SMTP_PASSWORD}\"
  }" | jq -r '.id')"
  echo "seed: created mail provider ${SMTP_ID}"
else
  echo "seed: mail provider exists (${SMTP_ID})"
fi

# Activation is idempotent in effect but not in status: activating one that is
# already active answers 400 AlreadyActive, which is a success for our purposes
# and must not fail the job. `api` uses curl -sf, so the non-2xx is swallowed
# here deliberately.
if api POST "/admin/v1/smtp/${SMTP_ID}/_activate" '{}' >/dev/null 2>&1; then
  echo "seed: mail provider activated"
else
  echo "seed: mail provider already active"
fi

echo "seed: done"
