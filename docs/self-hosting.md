# Self-hosting Kindlast

Kindlast is AGPL-3.0 licensed and you are free to run your own instance. This
document covers what that actually takes.

Read the failure mode section first. It is the one thing that will bite you.

## The failure mode that matters

**Kindlast's background agents run on scheduled HTTP calls, not on an in-process
timer.** On Vercel these are declared in `vercel.json` and the platform calls
them. Anywhere else, nothing calls them.

If you deploy without setting up scheduling, the app looks completely healthy.
You can sign up, complete onboarding, and browse the console. No error appears
anywhere. But the Analyst never runs, no findings are ever created, and no
notification is ever sent. The product silently does nothing, which is a worse
outcome than crashing.

Set up scheduling. It is described below.

## Requirements

| | |
|---|---|
| Bun | 1.3 or later, for installing and running scripts |
| Node.js | 22.13 or later, since Next.js runs on it |
| Docker | For the bundled stack: Postgres, the OIDC provider, Redis, the edge |
| Postgres | 17 with pgvector, supplied by the stack |
| Scheduler | Anything that can make an authenticated HTTP GET on a cron schedule |

## The stack

Kindlast no longer depends on a hosted platform. Everything it needs runs from
one compose file, and the removal of that dependency is the point: you are not
signing up for anyone's cloud to run this.

```bash
docker compose -f deploy/compose.yaml up -d
```

That gives you Postgres with the tenancy role split applied, migrations run by
a job container that must exit zero, Zitadel serving OIDC discovery, Redis, and
a Caddy edge. Tear it down with `down -v`.

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
running stack and should not be filled in by hand. The resource server has its
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
the client at it and leave the `model` service out. Understand what that
changes: your compliance profile, findings and DSAR content start leaving the
deployment, and the provider becomes a processor you are responsible for
recording.

### Required for the full agent loop

| Variable | Why |
|---|---|
| `TAVILY_API_KEY` | Currently unused. Nothing calls `lib/websearch` since the Analyst was removed, and the stored corpus may have made it unnecessary; see ENT-240. Leave it unset unless you have restored a caller. |
| `NEXT_PUBLIC_APP_URL` | Absolute origin for links in outbound email. Falls back to the request's forwarded host, which is usually wrong behind a proxy. |

### Optional

| Variable | Default | Why |
|---|---|---|
| `EMAIL_PROVIDER` | `console` | `console` logs email instead of sending. Set to `resend` to actually send. |
| `RESEND_API_KEY` | | Only if `EMAIL_PROVIDER=resend` |
| `EMAIL_FROM` | | A verified sender on your own domain |
| `BILLING_PROVIDER` | `stripe` | Only relevant if you are charging for your instance |
| `STRIPE_SECRET_KEY`, `STRIPE_PRICE_ID`, `STRIPE_WEBHOOK_SECRET` | | Billing only |
| `WEBSEARCH_PROVIDER` | `tavily` | `firecrawl` is a stub |

You can run a useful instance with `EMAIL_PROVIDER=console` and no billing
configured at all. Findings will appear in the console, they simply will not be
emailed.

## Build and run

```bash
bun install --frozen-lockfile
bun run build
bun run start          # serves on :3000
```

Put a reverse proxy in front of it for TLS. Set `NEXT_PUBLIC_APP_URL` to the
public origin, not the internal one.

## Running the web app against the self-managed stack

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

**The console is being rebuilt, so expect it to be absent.** The dashboard,
feed, compliance records, settings, billing and onboarding pages were removed
with Supabase: their tenancy was Supabase's `auth.uid()` row level security,
and authentication no longer produces a Supabase session. What you get today is
the marketing site, the sign-in flow and an organisation's own page at
`/o/{slug}/`, which signing in resolves you into. Each surface returns on
core-api, and it is worth saying plainly rather than leaving you to discover
it: this is not yet a system you would run a compliance programme on.

## Scheduling the agents

This is the part Vercel does for you and nothing else does.

Every scheduled endpoint is a `GET` that authenticates with a bearer token:

```
Authorization: Bearer ${CRON_SECRET}
```

They fail closed. If `CRON_SECRET` is unset the route returns 401 rather than
running unauthenticated, so a missing secret shows up as "nothing happens"
rather than as an open endpoint.

The schedules, matching `vercel.json`:

| Endpoint | Schedule | Purpose |
|---|---|---|
| `/api/notifications/dispatch` | `*/5 * * * *` | Sends queued finding emails |
| `/api/notifications/briefing` | `0 * * * *` | Hourly tick, fires per user at their local Monday 09:00 |
| `/api/notifications/deadline-alerts` | `0 7 * * *` | Daily deadline warnings |

The briefing endpoint is hourly by design. It resolves each user's local Monday
morning itself, so do not "optimise" it to weekly.

### With crontab

```cron
CRON_SECRET=your-secret-here
APP=https://kindlast.example.com

*/5 * * * * curl -fsS -H "Authorization: Bearer $CRON_SECRET" $APP/api/notifications/dispatch >/dev/null
0   * * * * curl -fsS -H "Authorization: Bearer $CRON_SECRET" $APP/api/notifications/briefing >/dev/null
0   7 * * * curl -fsS -H "Authorization: Bearer $CRON_SECRET" $APP/api/notifications/deadline-alerts >/dev/null
```

`curl -f` makes a non-2xx response a non-zero exit, so cron's own mail will tell
you when something breaks. Do not silence stderr as well as stdout.

Systemd timers, Kubernetes CronJobs, GitHub Actions on a schedule, or any hosted
cron service all work equally well. The only requirement is an authenticated
HTTP GET on time.

### Verifying it works

```bash
curl -i -H "Authorization: Bearer $CRON_SECRET" \
  https://kindlast.example.com/api/notifications/dispatch
```

`200` means the loop is alive. `401` means `CRON_SECRET` does not match between
your scheduler and the app. A `404` means the route is not deployed.

## What is not provided

**There is no Dockerfile or docker-compose file.** Shipping an untested one
would be worse than shipping none, since it would look supported and then fail
in ways nobody here could reproduce. If you build one that works, a pull request
would be genuinely welcome.

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
