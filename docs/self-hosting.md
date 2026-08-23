# Self-hosting Kindlast

Kindlast is AGPL-3.0 licensed and you are free to run your own instance. This
document covers what that actually takes.

Read the failure mode section first. It is the one thing that used to bite,
and the paragraph is kept so you know what to check.

## The failure mode that used to matter

Until ENT-256, Kindlast's background agents ran on scheduled HTTP calls: on
Vercel the platform made them, and anywhere else nothing did. A deployment
without a scheduler looked completely healthy (sign up, onboard, browse) and
silently did nothing: the Analyst never ran, no findings were ever created, no
notification was ever sent.

**That failure mode is gone. The schedules are inside the stack.** The
`workers` container registers every schedule with Temporal when it boots, and
Temporal runs them wherever the stack runs, including air-gapped. There is no
cron to set up on the host and no endpoint to call on a timer. What remains
worth checking after an upgrade is that `workers` is running with
`KINDLAST_TEMPORAL_ADDR` set: a gateway-only `workers` says at boot that
nothing will run on a schedule, and `temporal schedule list` (below) shows
five schedules when all is well.

## Requirements

| | |
|---|---|
| Bun | 1.3 or later, for installing and running scripts |
| Node.js | 22.13 or later, since Next.js runs on it |
| Docker | For the bundled stack, which includes the console itself. On a host that only runs Kindlast, this is the only requirement in this table |
| Postgres | 17 with pgvector, supplied by the stack |

## The stack

Kindlast no longer depends on a hosted platform. Everything it needs runs from
one compose file, and the removal of that dependency is the point: you are not
signing up for anyone's cloud to run this.

```bash
docker compose -f deploy/compose.yaml up -d
```

That gives you Postgres with the tenancy role split applied, migrations run by
a job container that must exit zero, Zitadel serving OIDC discovery, Redis, the
resource server, the integrations gateway, Temporal, a Caddy edge, and
**Kindlast itself at http://localhost:8000**. Tear it down with `down -v`.

The console is a production Next build in its own container (ENT-241), so a
host running this needs Docker and nothing else: no Bun, no Node, no checkout
of this repository beyond the compose file and what it references. Until that
service existed the stack gave you everything the product needs and not the
product, and the only way to see Kindlast was to run a development server by
hand, which is a maintainer's workflow rather than a deployment.

It publishes no port of its own. The edge is the front door, so one address is
the whole instance, and that is where you put TLS and a real hostname. It is
also what keeps port 3000 free for `bun run dev`, which is the workflow the
section below is about.

**The role split is not decoration and should survive any substitution you
make.** A plain Postgres superuser bypasses row level security entirely, and so
does a table owner unless the table forces it. The failure mode is silent:
every query succeeds, and tenant isolation is simply absent. So the application
connects as a role that owns nothing, cannot bypass RLS, and holds table-level
grants only. If you point Kindlast at your own Postgres, reproduce that
separation before anything else, and run `bun run test:db` to check you did:
it asserts the properties over `pg_class` rather than trusting configuration.

Schema changes flow through goose migrations in `db/migrations/`.

## Environment variables

`.env.example` is the authoritative list and is commented in more detail than
this table. What follows is the required-versus-optional split, which is the
part that is hard to work out from the file alone.

The web app's own OIDC settings are written by `./scripts/web-env.sh` from the
running stack and should not be filled in by hand. That script is for a
development server on the host: the containerised console needs none of it,
because compose points it at the same file in the same volume the script copies
out of. The resource server has its
own settings, including how to point it at an IdP other than the bundled one,
in [docs/core-api-configuration.md](./core-api-configuration.md). One thing
there is worth knowing before you deploy rather than after: the OIDC issuer is
part of every derived user id, so changing it later is an identity migration
and not a settings edit.

### Required

| Variable | Why |
|---|---|
| `NOTIFICATION_TOKEN_SECRET` | Signs one-tap approve, reject and unsubscribe links. Must be a long random string. |
| `CRON_SECRET` | Bearer token the scheduled endpoints require. Must be a long random string. |

Generate the two secrets with something like `openssl rand -hex 32`. They are
independent and should not be the same value.

**There is no required API key for the model.** That row used to read
`OPENAI_API_KEY`, "nothing intelligent works without it", and it is gone
because the stack now runs a model itself. See the next section.

Both are for surfaces currently being rebuilt: the notification and scheduled
paths went with the Supabase removal, so nothing reads these yet. They are
listed because the values should be settled before the surfaces return, not
improvised on the day.

### Temporal

The workflow engine runs on `postgres-platform` beside Zitadel, never on the
domain database, and these are the only settings it takes. All have local
development defaults, and the first is the one to change before anything
leaves a laptop.

| Variable | Default | Why |
|---|---|---|
| `KINDLAST_TEMPORAL_DB_PASSWORD` | `temporal-db-dev-password` | The password for Temporal's own role on `postgres-platform`. The `temporal-init` job creates the role and its two databases (`temporal`, `temporal_visibility`) with it, and the engine connects with it. |
| `KINDLAST_TEMPORAL_RETENTION` | `168h` | How long a finished workflow's history is kept. **Applied when the default namespace is first created and not changed by editing this afterwards.** A workflow history carries finding ids and enough context to re-identify an organisation, so this is a retention decision about personal data rather than a log setting: keep it well short of how long you keep the audit log, because the legal record lives in the domain database and the execution telemetry must not become a second one. To change it on an existing deployment: `docker compose -f deploy/compose.yaml exec temporal temporal operator namespace update --retention 72h default`. |
| `KINDLAST_TEMPORAL_UI_PORT` | `8233` | Host port for the Temporal UI, which is in the `dev` profile and published only when that profile is on (see below). |

The UI is how you read why a workflow is stuck, and it is deliberately not
part of a default `up`, because every history it shows carries finding ids:

```bash
docker compose -f deploy/compose.yaml --profile dev up -d
# then http://localhost:8233
```

Nobody writes migrations for Temporal's databases. The engine runs its own
schema tool on boot, against databases it owns, and an upgrade of the image is
an upgrade of the schema. `temporal-init` runs on every boot and creates only
what is missing, so a deployment that predates Temporal gets its role and
databases the first time it starts the new stack, with nothing to run by hand.

### The model

**The stack runs its own model and needs no API key to do it.** That is the
default and it is the point: a deployment holding a compliance record can run
with no outbound internet at all.

**It is opt-in, because it is gigabytes:**

```bash
docker compose -f deploy/compose.yaml --profile model up -d
```

That starts two more services. `model-init` fetches the weights once, checks
them against a pinned SHA256 and exits; `model` is llama.cpp's `llama-server`,
OpenAI-compatible on port 8081.

Without `--profile model` you get the rest of the stack and no model, which is
a supported configuration rather than a broken one: the console runs and
onboarding degrades to a form. It is the right default for CI and for anyone
who wants the database without a multi-gigabyte download, and the wrong one for
an actual deployment, so a real install passes the flag.

| Variable | Default | Why |
|---|---|---|
| `KINDLAST_MODEL_TIER` | `4b` | `2b`, `4b` or `9b`. Picks filename, URL and digest together, which is why it is one variable rather than three. |
| `KINDLAST_MODEL_CTX` | `16384` | Context window. A **memory** decision, not a capability one: the model supports 262144 natively, and allocating that would want far more RAM than the weights. |
| `KINDLAST_MODEL_PARALLEL` | `2` | Concurrent slots. |
| `KINDLAST_MODEL_PORT` | `8081` | Host port for the endpoint. |
| `KINDLAST_MODEL_ENDPOINT` | `http://model:8080` (on core-api) | Where core-api sends the deployment's own completions. **core-api makes every model call** (ENT-256, part five): the Python service asks core-api for each completion, naming only the organisation, and core-api resolves whether that organisation uses this endpoint or a provider it chose, opens the provider key only it holds, and dials. The Python service holds no model endpoint and no key. Empty means this deployment runs no model; a completion is then refused with a reason and nothing dials anything. Not `KINDLAST_MODEL_URL`, which is `model-init`'s download URL. |

Sizing, so you can pick before rather than after:

| Tier | Download | RAM at 4-bit |
|---|---|---|
| `2b` | 1.2 GB | ~3.5 GB |
| `4b` (default) | 2.7 GB | ~5.5 GB |
| `9b` | 5.7 GB | ~6.5 GB |

The weights land in `deploy/models/` on the host, not in a Docker volume, so
`docker compose down -v` does not throw them away. Re-running `up -d` with the
file already present verifies the digest and exits in under a second.

**Air-gapped installs.** Put the `.gguf` in `deploy/models/` yourself and
symlink it to `deploy/models/current.gguf`. `model-init` verifies what it finds
and never opens a socket. To supply a model that is not one of the three tiers,
set `KINDLAST_MODEL_FILE`, `KINDLAST_MODEL_URL` and `KINDLAST_MODEL_SHA256`
**together**; setting only some of them is refused rather than merged, because
a filename from one model and a digest from another is a configuration that
deletes a good file and fetches the wrong one.

**Using a hosted provider instead.** Anything OpenAI-compatible works, so point
`KINDLAST_MODEL_ENDPOINT` on core-api at it and leave the `model` service out.
Understand what that changes: your compliance profile, findings and DSAR
content start leaving the deployment, and the provider becomes a processor you
are responsible for recording. (A deployment-wide hosted endpoint that needs a
key is not supported through this setting: the deployment's own endpoint is
dialled without one. An organisation that wants a keyed provider chooses it
per organisation, below, and core-api holds the key.)

### Letting an organisation choose its own provider (ENT-236)

`KINDLAST_MODEL_URL` above is a decision for the whole deployment. This is the
other half: one organisation inside it choosing a hosted provider while the
rest keep using the model you run.

**It is off unless you switch it on, and that default is the point.** With
`KINDLAST_BYOK_PROVIDERS` unset, nobody in this deployment can point their
compliance data at an external API, whatever any of their owners decide. If
"nobody at this company may do that" is your position, the way to hold it is to
leave this alone, and nothing in the product can override it.

```
KINDLAST_BYOK_PROVIDERS=openai=api.openai.com,azure=.openai.azure.com
```

**`name=host`, not a bare name.** The host is what an endpoint is checked
against, so a list of names alone would let an organisation pick a permitted
provider and point the endpoint anywhere. A leading dot makes an entry a
suffix, so `.openai.azure.com` permits a customer's own Azure resource without
permitting every host that ends in those characters. A malformed entry stops
core-api at boot rather than leaving you with a deployment that permits nothing
while its configuration says otherwise.

You also need `KINDLAST_INTEGRATION_KEY` set, because a provider key is sealed
with the same keyring as an integration credential. Without one, a provider
that needs a key is refused rather than stored in plaintext.

**What happens then.** An owner, and only an owner, sees the choice under
Settings, is shown in plain language what changes, and has to confirm it before
anything is written. The change lands in that organisation's `audit_log`
alongside every other decision, so it lists, filters and exports with the rest
of their record, and every agent run from then on records which provider served
it. Turning it back off destroys the stored key in the same statement that
revokes the choice; it cannot reach content the provider has already processed,
and the product says so rather than implying otherwise.

**What is checked, and what is not.** Every endpoint is required to be HTTPS,
to be on the host you permitted, and to resolve to a public address: private,
loopback and link-local answers are refused, and one private answer among
several refuses the lot. Those checks run again on every use rather than once
when the endpoint is saved, so a provider you remove from this list stops being
reachable for organisations that already chose it. What is not closed is DNS
rebinding between the check and the request; what bounds it is that the host
had to be on your list in the first place.

### Required for the full agent loop

| Variable | Why |
|---|---|
| `NEXT_PUBLIC_APP_URL` | Absolute origin for links in outbound email. Falls back to the request's forwarded host, which is usually wrong behind a proxy. |

### Optional

| Variable | Default | Why |
|---|---|---|
| `EMAIL_PROVIDER` | `console` | `console` logs email instead of sending. Set to `resend` to actually send. |
| `RESEND_API_KEY` | | Only if `EMAIL_PROVIDER=resend` |
| `EMAIL_FROM` | | A verified sender on your own domain |
| `BILLING_PROVIDER` | `stripe` | Only relevant if you are charging for your instance |
| `STRIPE_SECRET_KEY`, `STRIPE_PRICE_ID`, `STRIPE_WEBHOOK_SECRET` | | Billing only |
| `WEBSEARCH_PROVIDER` | `firecrawl` | Picks the `lib/websearch` provider. Nothing calls that module, so this changes nothing at runtime. |
| `FIRECRAWL_API_URL` | | Base URL of your own Firecrawl instance, `http://firecrawl:3002` for the self-hosted image. The only websearch configuration that can run air-gapped. |
| `FIRECRAWL_API_KEY` | | A key for Firecrawl's hosted API instead. The self-hosted image runs with authentication off, so it usually needs no key. |
| `TAVILY_API_KEY` | | **Not required, whatever an older copy of this page told you.** Nothing calls `lib/websearch`, so leave it unset. |

**`TAVILY_API_KEY` used to be listed as required and it never was.** This page
told you to go and get a third-party search key before your stack would work
properly, and that was simply wrong: `lib/websearch` has had no caller since
the Analyst was removed along with the Supabase-era console, so no request path
ever reaches it and no key is ever read. If you obtained a Tavily key on the
strength of the old table, you did not need it and you can drop it. Nothing
degrades without it, because nothing used it.

**The websearch default is now Firecrawl** (ENT-240), and that is a statement
about this stack rather than a preference between two vendors. Firecrawl's
engine is AGPL-3.0 and you can run it yourself; Tavily's is closed and hosted,
with no self-hosting path, so a deployment with no outbound internet cannot use
it at all. Configured with neither an instance URL nor a key, the provider
refuses rather than quietly reaching for a hosted API. Still: nothing calls the
module, so this is the shape the seam will have when something does, not a
thing you have to configure today.

You can run a useful instance with `EMAIL_PROVIDER=console` and no billing
configured at all. Findings will appear in the console, they simply will not be
emailed.

## What leaves the deployment

This is the whole egress surface in one place, so you can see it without
grepping for it. It is here because a compliance record is exactly the kind of
data whose operator should be able to answer "what does this thing phone home
to" without reading the source.

**A default install, once it is built and running, makes no outbound request
at all.** Everything below is either a one-off at install time, or something
you have to switch on.

| What | When | On by default | How to do without it |
|---|---|---|---|
| Docker image pulls | First `up` | Yes | Mirror the images into your own registry and pull from there |
| Model weights, via `model-init` | First `up --profile model` | Only with the profile | Put the `.gguf` in `deploy/models/` yourself. `model-init` then verifies what it finds and never opens a socket |
| Google Fonts | `bun run build` | Yes | Nothing to do at runtime. `next/font/google` downloads the files during the build and serves them from your own origin, so the running app never asks Google for anything |
| Customer MCP servers, via the workers gateway | Request time | No | Nothing to do. `KINDLAST_GATEWAY_EGRESS_ALLOWLIST` is empty unless you set it, and an empty allow-list refuses every fetch |
| Resend, for email | Sending a notification | No | The default is `EMAIL_PROVIDER=console`, which logs instead of sending |
| A hosted model API, for the whole deployment | Every agent run | No | The default `KINDLAST_MODEL_URL` is `http://model:8080`, the llama.cpp service in your own stack |
| A hosted model API, chosen by one organisation | Every agent run for that organisation | No | `KINDLAST_BYOK_PROVIDERS` is empty unless you set it, and an empty list permits no provider to anybody. See "Letting an organisation choose its own provider" above |
| `va.vercel-scripts.com`, via `@vercel/analytics` | Page load, development builds only | Development only | See the note below |
| A Firecrawl or Tavily instance, via `lib/websearch` | Never today | No | Nothing calls the module. Configured with neither `FIRECRAWL_API_URL` nor a key, it refuses rather than reaching for a hosted API, and the default provider is the one you can run yourself |

Two things that look like egress and are not. **Stripe** never receives a
request from this stack: billing is applied by verifying a signed webhook that
Stripe sends to you, so the traffic is inbound. **The regulatory corpus** is
committed to the repository as JSON under `data/corpus/` and loaded into
Postgres locally, so nothing is fetched to answer a question about the law. The
obligation page links out to EUR-Lex rather than fetching it, which is a link
your browser follows if you click it and not a request the server makes.

**The analytics component deserves a straight answer**, because `<Analytics />`
sits unconditionally in `app/layout.tsx` and that looks worse than it is. In a
production build it loads `/_vercel/insights/script.js` and posts events to
`/_vercel/insights/event`, both on your own origin. Those paths exist only
because Vercel's edge answers them, so on a self-hosted deployment they 404 and
no data reaches anyone. Development builds are the exception: there the script
comes from `va.vercel-scripts.com`. If you would rather not rely on a 404,
delete the component from the layout. Nothing else references it.

### Air-gapped operation

**Running with no outbound internet is a supported mode and a deliberate
property rather than an accident.** It is worth stating because it disciplines
future decisions: the next feature that reaches for a hosted API should know it
is spending something.

Exactly two things are ever permitted to leave, and both are inbound fetches an
operator configured rather than ambient traffic:

1. **Keeping the regulatory corpus current.** The law changes and a deployment
   that cannot learn that is worse than one that can.
2. **The organisation's own data, arriving over MCP**, through the workers
   gateway and its allow-list.

Neither is on by default and neither is needed to run the console.

**The mode is proven by a test rather than by an audit** (ENT-240). It used to
be the latter: somebody had read the source and believed it, which is true on
the day it is written and silent every day after. Now CI brings the whole stack
up on a network with no route out and checks that the console still answers:

```bash
bun run test:airgap
```

That is `scripts/airgap-check.sh`, and it takes a few minutes because it brings
your stack up twice. What it does is worth knowing before you trust it. It
brings the stack up normally, so images are pulled and `web` is built, and
those are the install-time fetches in the table above. Then, from a container
on the stack's own network, it reaches a public address and requires that to
**succeed**: a machine with no internet cannot demonstrate anything by failing
to reach the internet, so a run that cannot pass this step skips rather than
reporting a pass it did not earn. Then it recreates the same stack with
`deploy/compose.airgap.yaml`, which marks the network `internal: true`, and
requires the same request to **fail** and the console to still serve.

Two things it does not cover, both deliberate. Pulling images and building the
console happen before the network is closed, because they are the install-time
egress the table names. And it does not watch for a service that tries to reach
out and copes quietly with being refused: it proves the stack works without
egress, not that nothing ever attempts any.

One more thing to know: `lib/websearch` is the seam through which a corpus
refresh would fetch. It has no caller today. Its default provider is Firecrawl,
which you can run yourself and therefore inside the air-gap, and **nothing it
reads is required for anything**, so an unconfigured deployment fetches
nothing.

## Build and run

The stack builds and runs the console for you, so this section is for running
it outside compose: on a platform that builds from source, or beside a stack
you started with `--no-deps` for some reason of your own.

```bash
bun install --frozen-lockfile
bun run build
bun run start          # serves on :3000
```

Put a reverse proxy in front of it for TLS. Set `NEXT_PUBLIC_APP_URL` to the
public origin, not the internal one.

To build the same image compose builds:

```bash
docker build -f apps/web/Dockerfile -t kindlast-web .
```

The build context is the repository root rather than `apps/web`, because the
install is driven by the root manifest and its lockfile. `NEXT_PUBLIC_APP_URL`
is the one setting that has to be supplied at build time, since Next inlines
`NEXT_PUBLIC_*` into the compiled output; everything else, including the OIDC
issuer and the OAuth client, is read at runtime so one image serves any
deployment:

```bash
docker build -f apps/web/Dockerfile \
  --build-arg NEXT_PUBLIC_APP_URL=https://compliance.example \
  -t kindlast-web .
```

## Running the web app against the self-managed stack

The stack already serves the console at the edge, so this section is about the
other console: a development server on port 3000, which is what you run when
you are changing the app rather than operating it. Both can be up at once, and
the seed registers an OAuth redirect URI for each so a sign-in works on either.

The stack in `deploy/` runs its own identity provider, so the web app needs
the OAuth client the seed job created in Zitadel. Those credentials are
generated per environment and written to a docker volume rather than to the
repository, which means there is no host-side path to them. `web-env.sh` is
that path:

```bash
docker compose -f deploy/compose.yaml up -d
./scripts/web-env.sh          # writes apps/web/.env.local
bun run dev
```

Re-run it after `docker compose down -v`. That discards the volume, so Zitadel
issues a new client on the next boot and a stale `.env.local` starts failing
with `invalid_client`, which is the likeliest cause of a sign-in that worked
yesterday and does not today.

**core-api is reached through the edge**, on `KINDLAST_EDGE_PORT` (default
8000), which routes `/kindlast.core.v1.*` to it. core-api publishes no port of
its own, deliberately: a caller on the host and a caller in another container
come through the same door, so there is no development-only shortcut that
stops existing in production.

### Signing in end to end

```bash
bun run --cwd apps/web test:e2e
```

That drives the development server. To drive the console the stack itself
serves, which is the artefact you would actually be running, name it:

```bash
KINDLAST_WEB_URL=http://localhost:8000 bun run --cwd apps/web test:e2e
```

The two are not interchangeable. `next dev` compiles on demand and forgives
things a production build does not, so a page that only fails when built is
invisible to the first command and caught by the second.

This drives a real authorization code flow: a browser, Zitadel's hosted login,
the redirect back, and the provisioning call that gives a new person their
first organisation. It creates its own throwaway users through Zitadel's
management API with the email already verified, which keeps the suite fast and
independent of message delivery rather than working around a broken stack.

### Mail

Mail works on this stack. The seed job configures Zitadel to deliver to the
bundled Mailpit, so registration, verification and password reset all complete,
and every message is readable at `http://localhost:8025`.

One trap is worth knowing, because it cost an afternoon and the error points
somewhere else entirely. **Zitadel refuses to use an SMTP provider that has no
credentials, and reports that refusal as `Errors.SMTPConfig.NotFound`.** The
config is not missing: `ListSMTPConfigs` returns it, the projection row is
present with state active, and the notifier still declines it
([zitadel/zitadel#8344](https://github.com/zitadel/zitadel/issues/8344)). The
symptom is a sign-up that completes with no message ever arriving and a single
line in the `auth` container's log.

So the seed sets a username and password even though Mailpit wants neither,
which is why Mailpit runs with `MP_SMTP_AUTH_ACCEPT_ANY`: any credentials are
accepted, so the values are arbitrary and development-only. Pointing at a real
SMTP server means replacing them, along with `SMTP_HOST` and the sender
address, in the `seed` service's environment.

**The console is served now, and it is still being rebuilt, so expect pages to
be missing rather than the application.** The dashboard,
feed, compliance records, settings, billing and onboarding pages were removed
with Supabase: their tenancy was Supabase's `auth.uid()` row level security,
and authentication no longer produces a Supabase session. What you get today is
the marketing site, the sign-in flow and an organisation's own page at
`/o/{slug}/`, which signing in resolves you into. Each surface returns on
core-api, and it is worth saying plainly rather than leaving you to discover
it: this is not yet a system you would run a compliance programme on.

## Scheduling the agents

Scheduled work runs inside the stack, on Temporal. There is no cron to set up
on the host and no endpoint to call on a timer; the schedules are part of the
deployment, and they run wherever the stack runs, including air-gapped.

What this page used to say here described three Vercel Cron routes and a
`CRON_SECRET`. Those went with the Supabase console and nothing reads that
secret now. If you still have a crontab entry pointing at
`/api/notifications/dispatch`, remove it: the route does not exist and the job
does nothing but fail.

**What runs on a schedule today**, stated rather than implied:

| Schedule id | When | What it does |
|---|---|---|
| `expire-snoozed-findings` | Hourly at ten past (`KINDLAST_SNOOZE_EXPIRY_SCHEDULE`) | Brings back every finding whose deferral has run out, in every organisation, so "defer for seven days" means seven days rather than until somebody remembers. One pass, idempotent, run as the producer role. |
| `relay-transactional-outbox` | Every 15 seconds (`KINDLAST_OUTBOX_RELAY_INTERVAL`) | Asks core-api what is waiting to leave and starts one workflow per row: `deliver-message/{row id}` for an invitation email, `deliver-notification/{row id}` for a finding notification. Each has a retry policy that backs off (ten seconds, doubling, capped at ten minutes) and no attempt limit: every attempt and what the mail server answered is in that workflow's history. A message that is not leaving is a running workflow in the UI with a reason. |
| `reclaim-transactional-outbox` | Hourly at forty past (`KINDLAST_OUTBOX_RECLAIM_SCHEDULE`) | Clears the recipient's address and the rendered body out of delivered messages after seven days, and gives up on undelivered ones whose invitation can no longer be accepted. The rendered body of an invitation carries the raw token, so this is a credential-lifetime pass before it is a data-minimisation one. A message that can still be delivered is never touched. |
| `relay-sweep-triggers` | Every 15 seconds (`KINDLAST_SWEEP_RELAY_INTERVAL`) | Asks core-api which sweeps somebody asked for and nothing has run (today: an onboarding somebody just confirmed writes a `sweep_triggers` row in the same transaction as the profile) and starts one `TriggeredSweepWorkflow` per row, named `sweep/{trigger id}`. So a fresh organisation sees findings within seconds of confirming, without anybody calling `RunSweep`. |
| `sweep-every-organisation` | Daily at 06:00 UTC (`KINDLAST_SWEEP_SCHEDULE`) | Lists every organisation with a compliance profile and runs the Watcher and then the Analyst over each, four at a time, as two activities per organisation with their own retries. One organisation's failure is recorded in the run's result and does not stop the rest. This is what pg_cron's `watcher-daily` and `analyst-daily` were; the Analyst is now the next step in the same workflow rather than a second job five minutes later. Then, one organisation at a time, it drafts the narrative for findings that have none (see below). |

Mail itself is sent by core-api, not by the worker: the worker asks core-api
to deliver a message by id, and core-api, which holds the SMTP channel, claims
the row, sends and records the outcome in one transaction. So a workflow
history carries row ids and counts, never an address, a subject or a body.
Without `KINDLAST_SMTP_ADDR` on core-api the deliveries retry with a reason
naming that setting, and drain on their own once it is set. Finding
notifications also need `KINDLAST_APP_BASE_URL` on core-api, for the link
into the console every one of them carries; without it they wait the same
way.

**A finding notification is one workflow that may live for hours**, and that
is the feature rather than a stuck run. The workflow asks core-api who should
hear about the finding and when: somebody whose preferences say "not inside
my quiet hours" is held on a durable timer until their window ends in their
own time zone, and told then. Until this change such a notification was
dropped with the reason on its row, because holding one needs a scheduler.
So a `deliver-notification/...` workflow showing as running overnight is a
person asleep, and its history says who was told, who is being held, and
until when. Nobody's address appears in it.

**Narration is the third step of every sweep, and it runs on two task
queues.** After the Watcher and the Analyst, and after a triggered sweep has
settled its trigger, the workflow narrates the organisation's findings that
have none, up to fifty per run, as three activities per finding: `workers`
(Go, the `core` queue) asks core-api for the next finding and the draft
request built for it; **the `intelligence` container (Python, the
`intelligence` queue) drafts it**, with every model call going back through
core-api's `CompletionService` so it holds no endpoint and no key; and
`workers` records the narrative or the refusal through core-api. Go loads,
Python drafts, Go persists, each retrying on its own. The feed shows every
finding with its deterministic text as soon as the sweep is done; explanations
arrive as they are drafted.

Three things an operator will see in a run's result: `Available: false` on a
stack without the `model` profile (one activity, nothing wrong); `Skipped`
naming the reason when an organisation's chosen provider cannot be honoured
(a withdrawn provider, a key that will not open), or when **no Python worker
is polling the `intelligence` queue**: the draft activity waits two minutes
for a worker and then the sweep records that and moves on, so an
`intelligence` container that is down costs explanations, never sweeps. The
Python worker starts with the container and needs `KINDLAST_TEMPORAL_ADDR`
(the bundled stack sets it); with it empty the container serves the RPC half
alone and says so at boot.

`SweepService.RunSweep` still exists for an operator who wants to sweep one
organisation now, with a service credential; see the Postman collection. It is
no longer how anything gets swept in the ordinary course of things. To sweep
the whole estate now rather than at 06:00, which is how to check an upgrade:

```bash
docker compose -f deploy/compose.yaml exec temporal \
  temporal schedule trigger --schedule-id sweep-every-organisation --address temporal:7233
```

The run's result in the UI says how many organisations were visited, how
many signals and findings came of it, and which organisations (by id) failed.

The worker that runs these is the `workers` container, the same binary as the
integrations gateway, polling the `core` task queue. It registers its
schedules with the engine at boot, so there is nothing to create by hand, and
it makes an existing schedule match its configuration if you change the cron.
Its settings:

| Variable | Default | Why |
|---|---|---|
| `KINDLAST_TEMPORAL_ADDR` | `temporal:7233` | Where the engine answers. **Empty means no worker**: the gateway half still runs and the container says at boot that nothing will run on a schedule. |
| `KINDLAST_TEMPORAL_NAMESPACE` | `default` | The namespace the schedules live in, which is the one auto-setup creates with the retention above. |
| `KINDLAST_TEMPORAL_TASK_QUEUE` | `core` | The queue this worker polls. |
| `KINDLAST_SNOOZE_EXPIRY_SCHEDULE` | `10 * * * *` | Five-field cron, UTC, for bringing deferred findings back. |
| `KINDLAST_OUTBOX_RELAY_INTERVAL` | `15s` | How often the relay looks for invitation mail waiting to leave. A Go duration. |
| `KINDLAST_OUTBOX_RECLAIM_SCHEDULE` | `40 * * * *` | Five-field cron, UTC, for clearing addresses and bodies out of delivered and abandoned messages. |
| `KINDLAST_SWEEP_RELAY_INTERVAL` | `15s` | How often the relay looks for sweeps somebody asked for. A Go duration. |
| `KINDLAST_SWEEP_SCHEDULE` | `0 6 * * *` | Five-field cron, UTC, for sweeping every organisation with a profile. |
| `KINDLAST_CORE_API_URL` | `http://edge:80` | Where the activities call. Through the edge, the same door Intelligence uses. |
| `KINDLAST_OIDC_*`, `KINDLAST_INTERNAL_CLIENT_FILE` | as core-api | The worker mints a token to call core-api, with the same service credential Intelligence presents. The bundled stack mounts the seed's files; an operator pointing at their own IdP sets these the way they did for core-api. |

### Seeing what is running

```bash
docker compose -f deploy/compose.yaml --profile dev up -d temporal-ui
# http://localhost:8233 shows every workflow, schedule and failure
```

To run a schedule now rather than at its next tick, which is how to check the
whole path after an upgrade:

```bash
docker compose -f deploy/compose.yaml exec temporal \
  temporal schedule trigger --schedule-id expire-snoozed-findings --address temporal:7233
```

A deferred finding whose date has passed comes back to "needs a decision" in
the feed within a few seconds, and the run's history in the UI says how many
moved.

To see an invitation email leave, invite somebody from the members page and
watch `temporal workflow list`: a `deliver-message/...` workflow appears within
the relay interval and completes when Mailpit (or your mail server) has
accepted the message. If it does not complete, its history says what the mail
server answered on each attempt, which is the question the old in-process
dispatcher could only answer from a counter in a table.

Or without the UI:

```bash
docker compose -f deploy/compose.yaml exec temporal \
  temporal workflow list --address temporal:7233
docker compose -f deploy/compose.yaml exec temporal \
  temporal schedule list --address temporal:7233
```

A workflow that has failed says why in its history, with every attempt and
every retry, which is the property that made Temporal worth a container: "why
did this finding get produced" and "why did it not" are both answerable from
the record rather than reconstructed from logs.

### Retention

See `KINDLAST_TEMPORAL_RETENTION` above. It is the one Temporal setting with a
data-protection consequence, and the default is chosen to be short.

## What is not provided

**There is no Dockerfile or docker-compose file.** Shipping an untested one
would be worse than shipping none, since it would look supported and then fail
in ways nobody here could reproduce. If you build one that works, a pull request
would be genuinely welcome.

## Backing it up

**Your deployment holds the compliance record, and nothing here backs it up for
you.** [docs/backup-and-restore.md](./backup-and-restore.md) covers what is
irreplaceable, why the two databases are one backup unit rather than two, and a
restore procedure that has been walked rather than only written.

Worth reading before you have data you care about rather than after. The short
version: `postgres-app` is the record, `postgres-platform` is the identity that
record's user ids are derived from, and losing either without the other leaves
you with organisations nobody can sign in to.

## Support expectations

Kindlast is developed for the hosted product at kindlast.com. Self-hosting is
supported in the sense that the licence permits it and this document exists, not
in the sense that there is an SLA.

- **Bug reports are welcome**, including self-hosting-specific ones, provided
  they come with enough detail to reproduce.
- **Configuration help is best-effort** through GitHub Discussions.
- **Security issues** follow [SECURITY.md](../SECURITY.md) regardless of how you
  run it.
- Your deployment, your provider credentials, and keeping your instance updated
  are yours to manage.

Security-relevant fixes are called out in release notes so you can assess your
own exposure.
