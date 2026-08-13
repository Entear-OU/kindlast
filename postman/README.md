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
docker run --rm -v kindlast_zitadel-machinekey:/k alpine cat /k/web-client.json
```

## What is in here, and what will not stay

Two halves, with different futures.

**Auth and web** describe endpoints Zitadel serves and the Next.js app
redirects to. They will never appear in a proto file, so this collection
stays their source of truth.

**Core API v1** describes the proto surface. Once `buf` emits OpenAPI
(design doc §23.2), those requests should be generated from the spec rather
than maintained by hand, or the two will disagree and the collection will be
the one that is wrong. Treat the hand-written ones as a stopgap.

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
