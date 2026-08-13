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
| Postgres | Via Supabase, which supplies auth, RLS, and pgvector |
| Scheduler | Anything that can make an authenticated HTTP GET on a cron schedule |

## Supabase

Kindlast depends on Supabase for more than a database: authentication, Row
Level Security policies, and pgvector for the regulatory corpus. A bare
Postgres instance will not work without significant modification.

Two options:

- **Supabase Cloud.** The free tier is enough to evaluate with.
- **Self-hosted Supabase.** See their [self-hosting guide](https://supabase.com/docs/guides/self-hosting).
  You are then running Kindlast and Supabase, which is a materially bigger
  commitment.

Once you have a project, apply the schema:

```bash
supabase link --project-ref <your-project-ref>
supabase db push
```

Migrations live in `supabase/migrations/` and are the only supported way to
change the schema.

## Environment variables

`.env.example` is the authoritative list and is commented in more detail than
this table. What follows is the required-versus-optional split, which is the
part that is hard to work out from the file alone.

The variables below configure the Next.js app against Supabase, which is the
stack this document describes. The self-managed stack in `deploy/` is being
built alongside it and is not yet what you would run; its resource server has
its own settings, including how to point it at an IdP other than the bundled
one, in
[docs/core-api-configuration.md](./core-api-configuration.md). One thing there
is worth knowing before you deploy anything rather than after: the OIDC issuer
is part of every derived user id, so changing it later is an identity
migration and not a settings edit.

### Required

| Variable | Why |
|---|---|
| `SUPABASE_URL` | Your project URL |
| `SUPABASE_PUBLISHABLE_KEY` | Client-side (anon) key |
| `SUPABASE_SECRET_KEY` | Server-side key. Never expose this to the browser. |
| `OPENAI_API_KEY` | Powers onboarding chat and compliance profile extraction. Nothing works without it. |
| `NOTIFICATION_TOKEN_SECRET` | Signs one-tap approve, reject and unsubscribe links. Must be a long random string. |
| `CRON_SECRET` | Bearer token the scheduled endpoints require. Must be a long random string. |

Generate the two secrets with something like `openssl rand -hex 32`. They are
independent and should not be the same value.

### Required for the full agent loop

| Variable | Why |
|---|---|
| `TAVILY_API_KEY` | The Analyst fetches verbatim regulatory text at citation time. Without it, citations degrade. |
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
