# The checks no suite runs

A short list of things to do by hand in a browser, and the reasoning for why
each one is here rather than in `surfaces.spec.ts` next door.

Everything in this file has the same shape: **the failure needs a state or an
artefact that a test run does not have.** A suite starts clean and builds what
it needs. That is what makes it repeatable, and it is exactly why it cannot
see these.

Run this list after a change to the proxy, the session store, the sign-in
flow, or the shape of a page's data loading. It takes about ten minutes.

## Why bother, given the e2e suite exists

`surfaces.spec.ts` was written because four surfaces had been green in every
test and broken in the browser. It crosses the boundaries those failures
crossed: a real browser, a real session, the edge, core-api, Postgres.

Two later failures got past it anyway, and neither was a gap in coverage in
the ordinary sense. They were failures the test pyramid is structurally unable
to reach.

**The lockout (ENT-223 follow-up, PR #157).** A session cookie that outlives
its session put the browser in a redirect loop between `/workspace` and
`/sign-in`, and the person was locked out of every page of the product
including the one that would have signed them back in. `curl` never saw it
because it sends no cookies. Playwright never saw it because it opens a fresh
context for every run. The bug lived precisely in the state where a cookie
already exists and has gone stale, and neither tool ever starts there.

**The unregistered handler (ENT-207).** `CorpusService` was absent from the
running core-api container while every Go test passed. Regulation rendered
"No regulation has been loaded" perfectly. The handler existed in the source
and in the binary the tests compiled; it was not in the binary that was
serving. A test that builds its own binary cannot catch a stale one.

So: prior state, and deployed artefact. The checks below are the two
categories, written out.

## Before you start

```bash
docker compose -f deploy/compose.yaml up -d
bash scripts/web-env.sh   # after any `down -v`, or sign-in fails with Errors.App.NotFound
bun run dev
```

`scripts/web-env.sh` is not optional after a teardown. `down -v` makes Zitadel
regenerate the web OIDC client, and a stale client id in `.env.local` surfaces
as a login form that never appears, which reads like a hang rather than a
configuration problem.

## Prior state: a cookie that outlives its session

The one that produced the lockout. Do this one first.

1. Sign in normally and land on `/o/{slug}/`.
2. Drop the session from Redis without touching the browser:
   ```bash
   docker compose -f deploy/compose.yaml exec redis redis-cli FLUSHALL
   ```
3. Reload `/workspace`.

**Expected:** the sign-in page renders, once. The URL carries `?returnTo=`.

**The regression:** `ERR_TOO_MANY_REDIRECTS`, or any number of hops between
`/workspace` and `/sign-in`. If you see it, the guard in `proxy.ts` that reads
`returnTo` has been broken or bypassed.

Repeat for a page deeper than the root, `/o/{slug}/logs` for instance. The fix
is deliberately central to the proxy rather than per page, and this is the
check that the centrality still holds: a per-page fix passes step 3 and fails
this one.

Then sign in again from that same page and confirm you land back on `/logs`
rather than the default surface. The loop breaker must not have eaten
`returnTo`.

## Prior state: two organisations in two tabs

Tenancy comes from the path on every request and is never remembered between
them. That is a security property, and the reason it is not a cookie is this
exact scenario.

1. Sign in as somebody who belongs to two organisations.
2. Open one in each of two tabs.
3. Navigate around in tab A.
4. Reload tab B.

**Expected:** tab B still shows organisation B. Nothing about tab A changed it.

**The regression:** tab B follows tab A. That means an active organisation is
being held somewhere per-session rather than read from the path, and a
consultant with three tabs open is being shown the wrong customer's
compliance record.

While you are here, type a slug the account does not belong to. It must be
**404, not 403**, and it must not redirect into one they do belong to. 403
confirms the organisation exists, which is a disclosure; a redirect tells them
which one they are in instead.

## Prior state: signing out of one tab

1. Two tabs, same organisation.
2. Sign out in tab A.
3. Reload tab B.

**Expected:** tab B goes to sign-in and stays there. This is the lockout check
again from a different direction, because signing out is the supported way of
producing a cookie that outlives its session.

## Deployed artefact: is the binary you are testing the one that is running

`core-api` publishes no ports, so both the host and the other containers reach
it through the edge, and there is no development-only shortcut. That means the
dev server is genuinely talking to the containerised core-api, and a handler
missing from that container shows up in the browser.

It does not mean the container is current. After any change under
`apps/core-api/` or `proto/`:

```bash
docker compose -f deploy/compose.yaml up -d --build core-api
```

Then check the specific surface the change touches. Green Go tests say the
handler compiles, not that it is registered in the image that is serving.

**The web half of this is not covered at all, and you should know that.**
There is no `web` service in `deploy/compose.yaml`, so there is nothing to
point a browser pass at except `bun run dev`. `next dev` is not
`next build && next start`: a server component that compiles in dev and fails
under a production build is invisible to every check in this repository. If
you are about to ship something that matters, run the production build by hand
and click through it:

```bash
bun run --cwd apps/web build && bun run --cwd apps/web start
```

Giving compose a `web` service would fold this into the stack properly. That
is real work rather than a note, and it is not scheduled.

## What is deliberately not here

**The CSV export.** `surfaces.spec.ts` covers it, and it downloads a file,
which is a poor fit for a checklist somebody runs quickly.

**Anything with an assertion that could be written.** If a check on this list
turns out to be expressible as a test, move it. This file is for the residue,
and it should stay short enough that people actually run it. A long checklist
is one nobody reads.
