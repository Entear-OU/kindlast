# Configuring `core-api`

How the resource server is configured, and the three decisions behind the
settings that look odd. Written for whoever is standing this up against an IdP
that is not the bundled Zitadel, and for whoever greps for one of these
variable names a year from now.

`core-api` fails closed on a missing required setting: it reports every one
that is absent and does not start. There is deliberately no default for the
audience, because a resource server that falls back to accepting some
convenient audience accepts tokens minted for a different service.

## Settings

| Variable | Required | Meaning |
|---|---|---|
| `KINDLAST_OIDC_ISSUER` | yes | The issuer as tokens carry it in `iss`. Also part of every derived user id, so see the warning below before changing it. |
| `KINDLAST_OIDC_AUDIENCE` | yes* | The audience this server accepts, and only this one. |
| `KINDLAST_OIDC_AUDIENCE_FILE` | no | Read the audience from a file instead. `KINDLAST_OIDC_AUDIENCE` wins if both are set. |
| `KINDLAST_OIDC_DISCOVERY_URL` | no | Where to fetch the discovery document, when that is not the issuer's own address. |
| `KINDLAST_OIDC_HOST_HEADER` | no | `Host` header to send to the authorization server. |
| `KINDLAST_OIDC_SCOPE_CLAIMS` | no | Comma-separated claim names to read granted scopes from, in addition to `scope` and `scp`. |
| `KINDLAST_DATABASE_URL` | yes | Must connect as `kindlast_app`. |
| `KINDLAST_REDIS_ADDR` | yes | The shared instance holding the revocation deny-list. |
| `KINDLAST_CORE_API_LISTEN` | no | Internal listener, default `:8080`. There is no public one. |
| `KINDLAST_BILLING_ENABLED` | no | Turns plan gating on. Off by default, which is the self-hosted case. |

\* one of `KINDLAST_OIDC_AUDIENCE` or `KINDLAST_OIDC_AUDIENCE_FILE`.

`KINDLAST_DATABASE_URL` must name `kindlast_app`: a role that owns nothing, is
`NOSUPERUSER` and is `NOBYPASSRLS`. Connecting as the migrator or the
superuser leaves every policy in the schema in place and makes every one of
them a no-op, with no error and no warning. `core-api` refuses to start if it
recognises either in the DSN, but that check is a courtesy and not a
guarantee, because a DSN can name any role.

## `KINDLAST_BILLING_ENABLED`, and why the default is off

Unset, `core-api` does not gate the act path on a subscription plan. That is
deliberate rather than an oversight: a self-hoster runs the whole product on
their own hardware, has no subscription and no intention of acquiring one, and
gating them out of the Executor because they are "on the free plan" would make
the self-hosted build a demo.

Set it (`1`, `true`, `yes` or `on`) and approving, rejecting or deferring a
finding requires an active `pro` subscription for the organisation. Anything
else, including a typo, leaves gating off, which is the safe direction for a
flag whose failure mode is refusing a paying customer their feature.

This is configuration rather than something inferred from whether a
subscription row exists. That inference is wrong in the direction that costs
money: a hosted customer between provisioning and their first row is
indistinguishable from a self-hoster under it.

## The `kindlast_agent` role

`core-api` connects as `kindlast_app` and always will. From ENT-203 the
database also has `kindlast_agent`, the role the Watcher and the Analyst run
as, and it exists because `kindlast_app` deliberately cannot create findings:
the thing that serves requests should not be able to fabricate a claim about a
customer's legal exposure.

`deploy/postgres/init/01-roles.sh` creates it on a fresh database. **An
existing deployment must create it before migration `00008` runs**, or the
migration fails with an explanation rather than skipping:

```sql
create role kindlast_agent login password '...'
  nosuperuser nocreatedb nocreaterole noinherit nobypassrls;
grant connect on database kindlast to kindlast_agent;
grant usage on schema public to kindlast_agent;
```

A local stack gets it from `docker compose -f deploy/compose.yaml down -v` and
back up. The password comes from `KINDLAST_AGENT_PASSWORD`.

Its policies are the only ones in the schema that omit the membership half of
the two-GUC form, because a sweep is started by the system and there is no
member to check. Tenancy still binds: the role can only touch the organisation
its `app.current_org_id` names, and it holds no grant on `organisations`,
`memberships` or `audit_log` at all.

## `KINDLAST_OIDC_SCOPE_CLAIMS`, and why it exists

The middleware reads the scope an RPC requires from the proto method option
and compares it against the scopes the caller's token carries. RFC 9068 says
those live in `scope`, space delimited, and some servers use `scp` instead.
Both are read by default and this setting is empty.

**The bundled Zitadel populates neither.** Measured against the seeded stack
rather than assumed: a client-credentials access token carries exactly `aud`,
`client_id`, `exp`, `iat`, `iss`, `jti`, `nbf` and `sub`. Requesting the
project audience scope, either roles reserved scope, or our own scope names
changes nothing, and neither does enabling project role assertion or granting
the machine user a role.

So the claim carrying granted authority is provider-specific, and naming it is
configuration rather than something this codebase should hold a table of.

**For the bundled Zitadel**, the settled approach is to enable
`accessTokenRoleAssertion` on the `web` application and set:

```
KINDLAST_OIDC_SCOPE_CLAIMS=urn:zitadel:iam:org:project:{projectId}:roles
```

substituting the project id, which the seed writes to
`core-api-audience.txt` on the shared volume. That configuration lands with
the OAuth client work rather than here, and machine-to-machine tokens are a
separate question still open: nothing populates a claim in a
client-credentials token, so service clients need verifying against a live
worker before they are relied on.

**For any other IdP**, point it at whichever claim that server populates:
Keycloak's `realm_access.roles`, Entra's `roles`, and so on.

### `openid` is the exception, and no configuration reaches it

One value in the vocabulary is not a permission. Every other scope answers
"may this client touch this kind of resource"; `openid` answers "did this
caller arrive through an OIDC login", and a token that has passed signature,
issuer, audience and expiry is exactly the proof of that.

No authorization server issues a grant for it, because it is a request flag
rather than a permission. Some servers, Keycloak among them, echo requested
scopes back into the token, which is an implementation detail rather than a
promise. So `libs/chassis/oidc` asserts it on every token it verifies, and no
value of `KINDLAST_OIDC_SCOPE_CLAIMS` changes that or needs to.

**The rule this creates: never declare `openid` on an endpoint that grants
authority. It means signed in, not permitted.** The endpoints that declare it
are bootstrap calls, reachable by a caller holding nothing, which they have to
be, because they are where a caller's first grant comes from.

### The three shapes accepted

A claim named here is read whether it is:

```jsonc
"scope": "openid findings:read"                    // space-delimited string
"roles": ["openid", "findings:read"]               // array of strings
"urn:zitadel:...:roles": {                         // object keyed by grant
  "findings:read": {"386089611182538755": "Kindlast"},
  "org:read":      {"386089611182538755": "Kindlast"}
}
```

The object case is not a convenience: it is the shape Zitadel and Keycloak
both emit for roles, where each key maps to the organisations the role was
granted in. Only the keys are read.

Values from every configured claim are unioned with the standard ones, rather
than the first non-empty answer winning, because a deployment may assert roles
in a vendor claim while still emitting `scope` for the OIDC basics.

Scope matching is always exact. `records:read` does not satisfy a requirement
for `records:ropa:write`, and it must not, now that the vocabulary splits per
resource.

## Reaching an IdP that is not where it says it is

`core-api` needs three facts about the authorization server, not one, and the
reason is visible in the bundled stack.

Zitadel advertises `http://localhost:8300` as its issuer, because that is
where a browser reaches it for the redirect flow. `core-api` runs on the
compose network, where no such address exists, and Zitadel routes by `Host`,
so a request that arrives without the right one reaches the wrong virtual
server rather than failing cleanly.

```yaml
KINDLAST_OIDC_ISSUER: http://localhost:8300                                  # what tokens will claim
KINDLAST_OIDC_DISCOVERY_URL: http://auth:8080/.well-known/openid-configuration  # where to fetch it
KINDLAST_OIDC_HOST_HEADER: "localhost:8300"                                  # who to ask for
```

The document must still declare the expected issuer or discovery is refused,
so this changes the address configuration is fetched from and never what is
trusted. Endpoints inside the document are then rebased onto the address they
were fetched from. That sounds like a liberty and is the opposite: the only
host ever contacted is the one an operator configured, rather than whichever
one a document happens to name.

Anyone running the IdP behind a reverse proxy, or on a different internal
hostname, needs these two optional settings. Leave both empty when the issuer
is reachable at the address it advertises, which is the ordinary case.

## What an access token has to carry, and what it does not

Two claims decide how well this works, and only one of them is required.

`sub` is required and is half of every user's identity. Everything else about
authorization comes from scopes, which need not live in the standard `scope`
claim; see `KINDLAST_OIDC_SCOPE_CLAIMS` above.

`name` and `email` are **not** required, and several providers omit them from
access tokens entirely. The bundled Zitadel is one: its access tokens describe
the grant and say nothing about the human. That matters exactly once per user,
when someone signs in for the first time and their personal organisation is
named after them.

So core-api asks the provider's **userinfo endpoint** instead, discovered from
the same document as the JWKS and called with the caller's own access token.
It happens only on the request that provisions an organisation, never on an
ordinary page render: putting the authorization server in the path of every
request is precisely what local token verification exists to avoid.

A provider that declares no `userinfo_endpoint` still works. core-api logs a
warning at boot, and a first organisation is then named from the `sub` claim,
which for Zitadel-style providers means a name like `386250729179840515`.
Usable, ugly, and worth knowing about before your users see it rather than
after.

## Changing the issuer is an identity migration, not a config edit

`memberships.user_id` is a `uuid`. Zitadel subjects are snowflake integers
such as `386089961457188867`, Auth0 issues `auth0|abc123`, and Entra issues
its own object ids. Only some IdPs happen to issue uuids.

So a subject that is not already a uuid is mapped to a stable version 5 uuid
derived from **the issuer and the subject together**. The issuer is included
deliberately: without it, two deployments federating different IdPs that
happened to issue the same subject string would collide onto one identity,
and the collision would show up as one person seeing another's organisations.

The consequence is worth stating plainly, because it is invisible until it
bites:

> **Changing `KINDLAST_OIDC_ISSUER` on a deployed instance re-derives every
> user id.** Every existing membership is then orphaned: the rows still exist,
> and nobody maps to them. Moving the IdP's public URL is an identity
> migration with a backfill, not a settings change.

Treat the issuer as set-once per instance. If it has to move, the old and new
derived ids both need computing and the `memberships` rows rewriting in one
transaction.

The derivation is also one-way, so a uuid cannot be turned back into a
subject. Provisioning therefore stores the raw `iss` and `sub` alongside the
derived id, which is what answers "who is this uuid" during an incident and
what a subject access request needs.

## Building the image

```bash
docker build -f apps/core-api/Dockerfile .
```

The build context is the repository root rather than `apps/core-api`, and the
Dockerfile copies `go.work` into it. That is a decision rather than an
accident.

`core-api` compiles against `gen/go` and `libs/chassis`. Neither is a
published module and neither is reached through a `replace` directive, so both
resolve **only** through the Go workspace. A build context rooted at the
service directory cannot see them, and a build without `go.work` cannot
resolve them.

The alternative was versioned pseudo-modules for both, which would mean
publishing and bumping two internal modules on every change to either. Copying
the workspace file keeps the no-replace rule intact and costs one line.

One consequence to know before it wastes your afternoon: **`go mod tidy`
cannot be run in these modules.** Tidy ignores workspaces by design, so it
tries to resolve `gen/go` and `libs/chassis` from the network and fails on a
private repository. Add dependencies with `go get`, which works in workspace
mode, and accept that the `// indirect` markers are maintained by hand.
