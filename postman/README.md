# Postman collection

The Kindlast HTTP surface, against the local stack. Committed so it is
reviewed with the code it describes and grows with the build order, rather
than drifting in one person's workspace.

## Using it

```bash
docker compose -f deploy/compose.yaml up -d
```

Import both files into Postman:

- `kindlast.postman_collection.json`
- `kindlast-local.postman_environment.json`

Run **Auth (Zitadel) → OIDC discovery** first. It asserts the issuer matches
and stores the advertised endpoints as variables, so later requests use the
paths the server publishes rather than paths copied in here by hand.

For anything needing a token, fill in `client_id` and `client_secret`. The
seed job generates them, so read them off the volume rather than guessing:

```bash
# the web OIDC client (authorization code + PKCE, needs a browser)
docker run --rm -v kindlast_zitadel-machinekey:/k alpine cat /k/web-client.json

# the client-credentials service user, which needs no browser (ENT-195)
docker run --rm -v kindlast_zitadel-machinekey:/k alpine cat /k/core-api-client.json
```

The volume is named for the compose project, and since ENT-250 that project is
per git worktree. In a single checkout it is `kindlast` and the commands above
are exact. In a worktree running its own stack, take the prefix from
`deploy/.env` (or `docker volume ls | grep zitadel-machinekey`) and change the
host and port in the environment file to match `./scripts/stack-env.sh
--summary`, or the collection will authenticate against a different branch's
Zitadel.

Two things about the client-credentials request are worth knowing before it
confuses you, both measured against this stack rather than taken from a doc.

**`client_id` is the service user's username, not its id.** The secret
response carries its own `clientId` and that is the one to use. Passing the
numeric user id returns `invalid_client: client not found`, which is a
misleading way to spend twenty minutes.

**The audience is Zitadel's project id.** Request the reserved scope
`urn:zitadel:iam:org:project:id:{projectId}:aud` or the token comes back
scoped to the client itself and `core-api` refuses it, correctly, as minted
for someone else. The project id is on the volume too, in
`core-api-audience.txt`, which is also where `core-api` reads it from.

A token so obtained reaches `core-api` and passes authentication. It is then
refused at the scope stage, because Zitadel's access tokens carry no `scope`
claim; see `scripts/validate-core-api-auth.sh`, which explains that gap where
it shows up.

## What is in here, and which half is generated

**Auth, web and Stack health** describe endpoints Zitadel serves and the
Next.js app redirects to. They will never appear in a proto file, so this
collection is their source of truth. Nothing generates them and nothing
rewrites them.

**Core API v1** describes the proto surface, and part of it is generated
(ENT-265):

```bash
bun run gen:postman          # or ./scripts/gen-postman.py
./scripts/gen-postman.py --check   # what CI runs, without writing
```

The generator owns three things per request, and nothing else: that a request
exists at all for every RPC in `proto/`, the Connect path it calls, and the
block below `**From the contract.**` in its description, which carries the
required scope and the declared REST binding. CI regenerates in the job that
already has `buf` and fails on any diff, so an RPC added without a request here
is a red build rather than a rule somebody remembered.

Everything else stays hand-written, because the contract does not carry it: the
request's name, its body, its headers, and the prose above the marker. Which
calls need `Kindlast-Org-Id` is the clearest case. It is not in the proto and
not derivable from the package, and seven requests contradict the obvious rule,
in both directions. A request that already exists keeps all of
it; a new RPC arrives with the proto comment as its prose, a `{}` body and a
guessed header set for a human to correct.

Two consequences worth knowing before they surprise you.

**Every description names a REST binding that nothing routes.** The proto
declares one per RPC, so `gen/openapi/openapi.yaml` and any client generated
from it will use `GET /api/v1/me` and its siblings. The edge routes the Connect
paths and opens exactly one `/api/v1` path, for the billing webhook. Opening
the REST surface is ENT-193, with a gateway, CORS policy and rate limiting
attached to that decision. From here, use the Connect path, which is what each
request already does.

**The generator does not reformat.** It reads the file into a tree that
remembers whether each object was written on one line and what bytes each
string was written with, and prints those back untouched, so a regeneration
touches only the requests it changed. `scripts/test_gen_postman.py` asserts
that, including that the assertion can fail.

## Requests marked NOT YET IMPLEMENTED

Deliberate. They document the contract the named issue will deliver, with
the reasoning that makes each one non-obvious: why logout is POST-only, why
invitation accept must run before the first `/api/v1/me`, why the active
organisation travels as a header rather than in the token. The collection
tracks the plan, not only the past.

They will fail until the issue lands. That is the intent.

## Secrets

None in these files, and none should be added. `client_secret` and
`access_token` are declared with empty values and typed `secret`, so
Postman keeps them out of exports. The client secret is generated per
environment by the seed job.

## What cannot be driven from Postman

The user sign-in flow is authorization code with PKCE, which involves a
login form and a redirect back to `web`. Use a browser for that. The
client-credentials request exists so API work does not need one.

## The gateway requests, which need a published port

The two `Gateway:` requests reach `apps/workers` rather than core-api, on
`gateway_base_url`. That service **publishes no port**: it answers core-api
on the compose network and nothing outside it should reach the process that
dials a customer's systems. So those two requests will not connect from a
host shell as the stack ships.

They are in the collection anyway, and deliberately. The gateway is the one
piece of this system whose behaviour a reader most needs to be able to
reproduce, and a collection that documented only the half core-api serves
would leave the refusals, which are the interesting part, undescribed.

To drive them, publish the port for as long as you need it:

```yaml
# deploy/compose.yaml, under `workers`. Not committed.
ports:
  - "127.0.0.1:8100:8100"
```

`gateway_token` in the environment matches the compose default and is a
development-only value. A real deployment mounts
`KINDLAST_GATEWAY_TOKEN_FILE` and publishes nothing.

`integration_id` is empty until you have connected something. Take it from
the response to `ConnectIntegration`.
