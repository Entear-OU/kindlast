#!/usr/bin/env bash
#
# Validate the core-api interceptor chain by hand (ENT-195).
#
#   ./scripts/validate-core-api-auth.sh            # against the running stack
#   ./scripts/validate-core-api-auth.sh --fresh    # tear down first, from empty volumes
#
# Every check below maps to an acceptance criterion on the issue, and each one
# prints what it proves rather than only whether it passed. The point is that a
# human can read the output and believe it, not that a script said OK.
#
# --fresh is worth using at least once. One criterion, that an empty JWKS at
# startup does not permanently break verification, can only be observed on a
# stack whose Zitadel has never issued a token: it generates its signing key
# lazily, so a fresh stack serves `{"keys": []}` and core-api caches nothing at
# boot. On an already-warm stack that check is skipped and says so.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

COMPOSE="docker compose -f deploy/compose.yaml"
AUTH="http://localhost:${KINDLAST_AUTH_PORT:-8300}"
NET=kindlast_default
CURL_IMG=curlimages/curl:8.11.1
CALL='http://core-api:8080/kindlast.core.v1.SessionService/GetCurrentUser'

passed=0
failed=0
skipped=0

step()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
pass()  { printf '  PASS  %s\n' "$*"; passed=$((passed + 1)); }
fail()  { printf '  FAIL  %s\n' "$*"; failed=$((failed + 1)); }
skip()  { printf '  SKIP  %s\n' "$*"; skipped=$((skipped + 1)); }
note()  { printf '        %s\n' "$*"; }

incurl() { docker run --rm --network "$NET" "$CURL_IMG" "$@"; }

# In a container, so no local psql/jq/go version matters for this one.
volread() { docker run --rm -v kindlast_zitadel-machinekey:/m alpine:3.21 cat "/m/$1" 2>/dev/null; }

FRESH=0
[ "${1:-}" = "--fresh" ] && FRESH=1

# ---------------------------------------------------------------------------
step "0. The stack"

if [ "$FRESH" = "1" ]; then
  note "tearing down (down -v destroys the local dev volumes, fixtures included)"
  $COMPOSE down -v >/dev/null 2>&1
fi

note "bringing the stack up; first run builds the core-api image"
if ! $COMPOSE up -d >/dev/null 2>&1; then
  fail "the stack did not come up; try: $COMPOSE up -d"
  exit 1
fi

for _ in $(seq 1 60); do
  status="$(docker inspect kindlast-core-api --format '{{.State.Health.Status}}' 2>/dev/null || echo none)"
  [ "$status" = "healthy" ] && break
  sleep 2
done
if [ "$status" = "healthy" ]; then
  pass "core-api is healthy"
else
  fail "core-api never became healthy (status: ${status})"
  note "logs: docker logs kindlast-core-api"
  exit 1
fi

# ---------------------------------------------------------------------------
step "1. The token battery, against a real JWKS with a keypair made in the suite"
note "valid allowed; wrong aud, alg:none, HS256-with-the-public-key, expired all denied;"
note "unknown kid refetches exactly once; an empty JWKS at boot still recovers."

if (cd libs/chassis && go test -count=1 ./oidc/... >/tmp/ent195-battery.log 2>&1); then
  pass "libs/chassis/oidc"
  note "$(grep -c 'PASS\|ok' /tmp/ent195-battery.log >/dev/null && echo 'see /tmp/ent195-battery.log for detail')"
else
  fail "the token battery is red"
  sed 's/^/        /' /tmp/ent195-battery.log | head -30
fi

# ---------------------------------------------------------------------------
step "2. Scope: rejected when undeclared, AND enforced at the declared value"
note "the second half is the one a reflection test cannot give you: a reader"
note "hard-wired to one scope passes every 'is a scope declared' check forever."

if (cd apps/core-api && KINDLAST_REQUIRE_STACK=1 go test -count=1 \
      -run 'Undeclared|EnforcesTheDeclaredValue|MissingFromTheTable' \
      ./internal/server/interceptor/... >/tmp/ent195-scope.log 2>&1); then
  pass "scope enforcement, both halves"
else
  fail "scope enforcement"
  sed 's/^/        /' /tmp/ent195-scope.log | head -30
fi

# ---------------------------------------------------------------------------
step "3. Tenancy against a real Postgres, as two users"
note "a cross-organisation read returns zero rows rather than an error, and"
note "current_setting('is_superuser') is off."

if (cd apps/core-api && KINDLAST_REQUIRE_STACK=1 go test -count=1 \
      ./internal/store/postgres/... >/tmp/ent195-tenancy.log 2>&1); then
  pass "tenant isolation through the code path that serves requests"
else
  fail "tenant isolation"
  sed 's/^/        /' /tmp/ent195-tenancy.log | head -30
fi

note "and the same tests must FAIL as a role that bypasses RLS, or they prove nothing:"
if (cd apps/core-api && PG_APP_URL="postgres://kindlast_migrator:${KINDLAST_MIGRATOR_PASSWORD:-migrator-dev-password}@127.0.0.1:${KINDLAST_PG_APP_PORT:-5433}/kindlast" \
      go test -count=1 ./internal/store/postgres/... >/tmp/ent195-bypass.log 2>&1); then
  fail "they PASSED as kindlast_migrator, so they are not detecting RLS at all"
else
  pass "they go red as kindlast_migrator (BYPASSRLS), so they are really checking"
fi

# ---------------------------------------------------------------------------
step "4. The whole chain, over the compose network"

published="$(docker inspect kindlast-core-api --format '{{json .NetworkSettings.Ports}}')"
if [ "$published" = "{}" ] || [ "$published" = "null" ]; then
  pass "core-api publishes no port (${published})"
  note "a leaked access token cannot be replayed against it from the internet"
else
  fail "core-api publishes ports, and must not: ${published}"
fi

if incurl -fsS http://core-api:8080/healthz >/dev/null 2>&1; then
  pass "it still answers on the internal network"
else
  fail "unreachable even inside the network"
fi

code="$(incurl -sS -o /dev/null -w '%{http_code}' -X POST "$CALL" \
  -H 'Content-Type: application/json' -d '{}' 2>/dev/null)"
if [ "$code" = "401" ]; then
  pass "no credential -> 401"
else
  fail "no credential -> ${code}, want 401"
fi

body="$(incurl -sS -X POST "$CALL" -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer not.a.token' -d '{}' 2>/dev/null)"
if printf '%s' "$body" | grep -q unauthenticated; then
  pass "a forged token -> unauthenticated"
else
  fail "a forged token -> ${body}"
fi

# ---------------------------------------------------------------------------
step "5. A real Zitadel token, and the empty-JWKS trap"

keys_before="$(curl -sS "${AUTH}/oauth/v2/keys" | jq -r '.keys | length')"
note "JWKS currently holds ${keys_before} key(s)"

CREDS="$(volread core-api-client.json)"
PROJECT_ID="$(volread core-api-audience.txt)"
if [ -z "$CREDS" ] || [ -z "$PROJECT_ID" ]; then
  fail "the seed did not write core-api-client.json / core-api-audience.txt"
else
  CLIENT_ID="$(echo "$CREDS" | jq -r .clientId)"
  CLIENT_SECRET="$(echo "$CREDS" | jq -r .clientSecret)"

  TOKEN="$(curl -sS -X POST "${AUTH}/oauth/v2/token" \
    --data-urlencode "grant_type=client_credentials" \
    --data-urlencode "client_id=${CLIENT_ID}" \
    --data-urlencode "client_secret=${CLIENT_SECRET}" \
    --data-urlencode "scope=openid profile urn:zitadel:iam:org:project:id:${PROJECT_ID}:aud" \
    | jq -r '.access_token // empty')"

  if [ -z "$TOKEN" ]; then
    fail "Zitadel issued no token for the seeded client"
  else
    pass "the seeded client-credentials client works (audience ${PROJECT_ID})"

    keys_after="$(curl -sS "${AUTH}/oauth/v2/keys" | jq -r '.keys | length')"
    if [ "$keys_before" = "0" ] && [ "$keys_after" -ge 1 ]; then
      pass "the signing key was generated only when the first token was issued"
      note "core-api booted against an EMPTY JWKS and still verifies the token below,"
      note "which is the whole point: the boot fetch must never be the last fetch"
    elif [ "$keys_before" = "0" ]; then
      fail "the JWKS was empty and stayed empty after a token was issued"
    else
      skip "the stack was already warm; re-run with --fresh to observe this"
    fi

    body="$(incurl -sS -X POST "$CALL" -H 'Content-Type: application/json' \
      -H "Authorization: Bearer ${TOKEN}" \
      -H 'X-Kindlast-Org: a0000000-0000-4000-8000-000000000001' -d '{}' 2>/dev/null)"

    if printf '%s' "$body" | grep -q unauthenticated; then
      fail "the real token was refused at authentication: ${body}"
      note "if this says 'unknown signing key', the refetch is not working"
    else
      pass "the real token passes signature, issuer, audience, expiry and jti"
      note "it is refused later in the chain, and by which stage matters:"
      note "  ${body}"
      note ""
      note "KNOWN GAP, and it is an IdP configuration question rather than a bug"
      note "here: Zitadel access tokens carry no 'scope' claim and no roles claim,"
      note "whatever reserved scope is requested. Measured, not assumed. So the"
      note "scope stage correctly refuses a token that declares no scopes at all."
      note "The verifier reads scope/scp per RFC 9068 and takes extra claim names"
      note "through KINDLAST_OIDC_SCOPE_CLAIMS, so pointing it at whatever claim"
      note "the IdP does populate is configuration, not a code change."
    fi
  fi
fi

# ---------------------------------------------------------------------------
printf '\n\033[1mResult\033[0m\n'
printf '  %d passed, %d failed, %d skipped\n' "$passed" "$failed" "$skipped"
[ "$failed" -eq 0 ] || exit 1
