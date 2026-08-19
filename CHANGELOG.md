# Changelog

Notable changes to Kindlast, in the format of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning follows
[Semantic Versioning](https://semver.org), and
[`docs/versioning.md`](./docs/versioning.md) says what a breaking change means
for this product.

Entries are written rather than generated. The audience is somebody upgrading a
system that holds their compliance record, so an entry says what changed and
what they have to do about it, which no commit subject knows.

## [Unreleased]

### Added

- **An organisation can choose its own model provider, and the choice is a
  compliance event rather than a preference** (ENT-236).

  The bundled stack runs its own model and needs no API key, which is what lets
  a deployment holding a compliance record run with no outbound internet at
  all. Pointing one organisation at a hosted provider is the act of giving that
  up: from then on its compliance profile, its findings and its DSAR content
  are processed by somebody else, which lengthens that customer's own
  sub-processor list and is a processing decision they have to be able to
  account for.

  So it is shaped as a decision, not a switch:

  - **Owner only**, and only when a person has been shown in plain language
    what changes and confirmed it. The confirmation is enforced by core-api and
    not by the console, so an API caller cannot skip the warning either.
  - **It writes an `audit_log` row** through the same function every other
    decision goes through, with the provider, the endpoint and who decided. It
    lists, filters and exports with the rest of the record, so a customer asked
    "since when has your findings text been going to a US provider" can answer
    from their own audit log.
  - **The choice cannot be edited**, only replaced. Switching provider revokes
    one row and inserts another, so the sequence of rows is the history of
    where that customer's data has been processed.
  - **Every agent run records which provider served it**, so the period a
    provider was in use is an answerable question rather than an inference.
  - **Turning it back off destroys the stored key** in the same statement that
    revokes the choice. It cannot reach content the provider already processed,
    and the product says so rather than implying an off switch is a recall.

  **For a self-hoster the important line is that this is off unless you switch
  it on.** `KINDLAST_BYOK_PROVIDERS` is empty by default and an empty list
  permits nobody, so "nobody at this company may point our compliance data at
  an external API" stays enforceable in your configuration rather than being a
  policy every organisation owner has to be trusted to follow. Entries are
  written `name=host` (`openai=api.openai.com`), because the host is what an
  endpoint is checked against; a leading dot makes an entry a suffix for a
  customer's own subdomain. Endpoints must be HTTPS, must be on the host you
  permitted, and must resolve to a public address, and all of that is checked
  again on every use rather than once when it is saved.
  [`docs/self-hosting.md`](./docs/self-hosting.md) has the whole of it.

- **A public readiness assessment at `/readiness`** (ENT-189). A visitor
  answers the questions a data protection officer would ask, with no account
  and no sign-up, and gets back the obligations in the Kindlast corpus that
  match their answers, each quoting the corpus entry and citing the regulation
  behind it.

  Three things a self-hoster should know about it, because they are properties
  of the deployment rather than of the page:

  - **It has no server side.** The corpus is compiled into the bundle at build
    time and the applicability rules are a pure function in the browser. There
    is no route handler, no server action and no call to `core-api`, so the
    page adds no unauthenticated surface to your deployment, needs no rate
    limit, and costs nothing per visitor beyond serving static output.
  - **It stores nothing, anywhere.** Answers live in one React state hook for
    the life of the tab. No database row, no cookie, no `localStorage`, no
    query string, and no request. A refresh loses the assessment, deliberately.
  - **Every statement of law on it is a corpus row, verbatim.** If you ship a
    modified `data/corpus/`, this page renders your text. If you ship no
    changes, it renders ours. Nothing on the page paraphrases a regulation, and
    a test fails the build if a string ever tries to.

  There is no email capture, which the issue asked for and which is held for a
  second iteration: sending the summary means putting somebody's address and
  their answers through a mail provider, and that needs a lawful basis, a
  notice, a retention position and an answer to a subject access request about
  the assessment itself, written down before the code rather than after it.

- **Air-gapped operation is now checked by CI rather than asserted in the
  docs** (ENT-240). `docs/self-hosting.md` has told you that a default install,
  once built and running, makes no outbound request at all. That was an audit
  of the source, and an audit is true on the day somebody writes it. Now every
  pull request brings the whole stack up on a network with no route out and
  fails if the console does not serve. Run it yourself with
  `bun run test:airgap`, or bring your own stack up that way with
  `docker compose -f deploy/compose.yaml -f deploy/compose.airgap.yaml up -d`
  once the images are built. It does not cover pulling images or building the
  console, which happen before the network closes and are named in the egress
  table.

### Changed

- **The `lib/websearch` default provider is now Firecrawl, not Tavily**
  (ENT-240), and Firecrawl is implemented rather than a stub that threw. This
  changes nothing at runtime, because nothing in the product calls that module,
  and it changes what a self-hoster is being pointed at when something does:
  Firecrawl's engine is AGPL-3.0 and you can run it yourself, so it works
  inside a deployment with no outbound internet, and Tavily's is hosted and
  closed, so it cannot. Set `FIRECRAWL_API_URL` to your own instance, or
  `FIRECRAWL_API_KEY` for the hosted API. With neither set the provider refuses
  rather than quietly reaching for a SaaS. Nothing here is required and nothing
  degrades if you ignore all of it.

- The local stack can run once per checkout instead of once per machine.
  `deploy/compose.yaml` still defaults to the project name `kindlast` and to
  the ports every instruction here names, so a self-hoster with one clone sees
  no change at all. What is new is that `COMPOSE_PROJECT_NAME`, the
  `KINDLAST_*_PORT` variables and `KINDLAST_MODEL_DIR` now reach every part of
  the stack, including container names, so a second copy of the repository can
  bring up a second stack that shares nothing with the first.
  `scripts/stack-env.sh` derives a consistent set of those values, and
  `docs/maintainers.md` explains it. Relevant to anyone running two
  environments from one machine, and to anyone whose tooling assumed a
  container was called `kindlast-postgres-app`: it still is, unless a project
  name is set.

### Fixed

- **The reason somebody gave for correcting a fact was stored and never shown
  again.** Correcting a fact asks why, and the placeholder suggests the shape
  of a useful answer ("We appointed a DPO in June"). That sentence was written
  to `org_profile_facts.note` and read back by nothing: `ProfileFact` had no
  field for it, so no response carried it and no page could render it. The
  question was asked, answered, and filed where only a database client could
  reach it.

  It is the part of the history that matters most. "What we used to think"
  could already show that a value changed and when, which is the easy half; why
  it changed is the half a person checking an older finding actually needs, and
  it is the reason the field was asked for.

  `ProfileFact` now carries `note`, and the history page renders it under the
  entry it belongs to, in the words it was written in. Notes recorded before
  this upgrade appear too: they were always stored, so nothing is lost and
  nothing needs backfilling.

- **Signing out did not sign anybody out of the identity provider, and ended
  on a raw JSON error page.** The seed registered `${origin}/` as the web
  client's post-logout redirect URI, while `/auth/logout` asks to return to
  `${origin}/sign-in`. An authorization server matches that list exactly, so
  Zitadel refused every sign-out with
  `{"error":"invalid_request","error_description":"post_logout_redirect_uri invalid"}`
  and left the person on its own domain looking at the JSON.

  The visible half was the harmless half. Refusing the request means
  `end_session` never ran, so the provider's session survived, while `web` had
  already destroyed its own session and cleared its cookie. The person looks
  signed out, and the next click on "Continue" signs them straight back in
  without ever asking for a password, which on a shared machine hands the
  workspace to whoever sits down next.

  The seed now derives `${origin}/sign-in` from each console's callback, so
  what is registered and what is requested cannot drift, and `journey.spec.ts`
  drives a real sign-out: it asserts both that the browser lands back on
  `/sign-in` and that signing in again reaches a password prompt.

  **Self-hosters must re-run the seed** for an existing stack to pick this up
  (`docker compose -f deploy/compose.yaml run --rm seed`). The seed republishes
  the client's OIDC configuration on every run, so no manual change in Zitadel
  is needed.
- **The Watcher could not complete a single sweep, so no deployment has ever
  produced a finding.** Two of the Watcher's three detectors read the `dsars`
  table, and the role the sweep runs as, `kindlast_agent`, was never granted
  anything on it. Every sweep failed on the first detector with
  `permission denied for table dsars` and returned an internal error, so no
  organisation ever got a finding, a feed entry, a notification or an Article
  30 record.

  It stayed hidden because nothing runs a sweep on a schedule yet, so the only
  way to trigger one is `SweepService.RunSweep` by hand, and a console that has
  never swept shows the same empty feed as one whose every sweep failed. The
  message on the feed, "the Watcher has not run for this organisation", was
  literally true and read as a state rather than as a symptom.

  The agent now holds `select` on `dsars` and nothing more, under a policy of
  the same shape as its other tenant tables: org equality against the one GUC a
  sweep sets, so a sweep pointed at one organisation still cannot see another's
  requests. The grant alone would not have been enough, and would have been
  worse: `dsars` forces row level security and its only select policy also
  requires a member, which a sweep deliberately does not set, so the sweep
  would have succeeded while silently finding no deadline for anybody.

  On upgrade, apply migrations and then run a sweep per organisation to
  populate feeds that have been empty. `db/tests/agent-role.test.ts` now calls
  `run_watcher()` as the agent itself, so the next detector that reads a table
  nobody granted fails on the commit that adds it.

- **Intelligence refused every request for the first minute of a fresh
  deployment** (ENT-253). A newly seeded authorization server has generated no
  signing key yet and serves an empty key set, which is correct rather than
  broken: it makes the key when it issues its first token. Intelligence
  fetched that empty set at boot, treated the fetch as having filled the cache,
  and then refused the first token it was ever shown with a 401 that read as
  "signed by a key I do not know". A minute later the cooldown lapsed and
  everything worked, so this was only ever visible on a stack that was seconds
  old, which is every stack a self-hoster starts for the first time.

  The boot fetch no longer counts as the cache's one permitted refetch, so the
  first token always reaches the network for a key it does not hold. `core-api`
  already worked this way. Nothing to do on upgrade beyond taking the new
  image.

- **Intelligence exited rather than starting when it came up before the
  authorization server.** Losing that race in a compose stack is ordinary, and
  the container did not come back on its own. It now logs a warning and starts,
  and the first token fetches the keys. A genuinely misconfigured issuer still
  fails at boot with a message naming the issuer, which is the case worth
  refusing to start for.

## [0.1.0]

The version the repository has carried in its manifests since the beginning,
recorded here so the history starts somewhere rather than appearing to begin
mid-sentence. No tag was cut for it, and this file does not attempt to
reconstruct the changes that led to it: the git history is the record for
everything before the first release.

[Unreleased]: https://github.com/Entear-OU/kindlast/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Entear-OU/kindlast/releases/tag/v0.1.0
