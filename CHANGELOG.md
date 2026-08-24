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

### Fixed

- **Console tabs and bookmarks now say which organisation they are showing**
  (ENT-269). Every page under `/o/{slug}/` inherited the marketing site's
  title, so a consultant with three client organisations open had three tabs
  reading "Kindlast: AI-Powered GDPR & AI Act Compliance" and a bookmark bar
  where nothing could be told apart. A console page now titles itself
  "{Organisation}, {Section}, Kindlast", organisation first because a tab
  strip truncates from the end and the organisation is the half that differs.

  Nothing to do on upgrade, and nothing changes on the marketing or sign-in
  pages. Two details worth knowing: a slug you do not belong to keeps the
  generic "Kindlast" title, so a 404 still says nothing about whether that
  organisation exists, and detail pages title themselves by section rather
  than by item, so a data subject's name never reaches a tab, a bookmark or
  browser history.

### Added

- **A fresh install comes up with the regulation in it** (ENT-266). Until now
  `docker compose up` from a clean checkout produced a deployment with an
  empty `regulatory_documents` and `regulatory_articles`: the Regulation page
  said no regulation had been loaded, and every obligation said the text
  behind its citation was not in this deployment. The corpus was committed
  under `data/corpus/` the whole time and nothing ever loaded it. A
  `corpus-load` job container now reads that directory and ingests it through
  the resource server, the same way `migrate` applies the schema and exits.

  **Nothing to run and nothing to configure.** The job mints its own token
  from the service credential core-api, Intelligence and the Temporal worker
  already share, so no new client, grant or secret arrives with it. It is
  idempotent, so a second `up` costs one pass and changes nothing.

  **It needs no route out.** It reads the JSON on disk and reaches only your
  own `auth` and `core-api`, so an air-gapped install gets the same corpus as
  any other. `bun run test:airgap` now waits for that job and fails if it
  could not finish, which is what stops the property being true today and
  quietly false later.

  **Updating the corpus later** is a `git pull` (or a newer release) and
  `docker compose -f deploy/compose.yaml up -d --build corpus-load`. That is
  the one piece of this that is an operator's decision rather than automatic:
  nothing polls for a newer corpus. `docs/self-hosting.md` has the section.

- **The Postman collection's Core API requests are generated from the proto
  contract, and CI fails when they drift** (ENT-265). `postman/` is the only
  executable description of the parts of this system that will never appear in
  a proto file, so it is worth trusting; until now the rule that kept its Core
  API half current was that every author remembered to mirror a proto change
  into it by hand. `./scripts/gen-postman.py` (also `bun run gen:postman`) now
  reads the same proto image `gen/openapi/openapi.yaml` comes from and owns
  three things per request: that a request exists at all for every RPC, the
  Connect path it calls, and the block below **From the contract.** in its
  description, which carries the required scope and the declared REST binding.

  **If you have imported this collection, re-import it.** Every Core API
  request's description gained that block, and the descriptions are where the
  measured facts live. Nothing else moved: no path, no header, no body.

  What stays hand-written is most of what makes the collection useful, and it
  stays that way deliberately. The authorization server's routes, `web`'s
  redirect endpoints and the webhook paths are untouched folders. Inside a
  generated request, the name, body, headers and the prose above the marker are
  hand-written, because the contract does not carry them: which calls need the
  active-organisation header is not in the proto, and seven requests contradict
  the obvious rule.

  **The REST binding each description now names is declared, not routed.** That
  was true before this change and is merely visible now. Every RPC carries a
  `google.api.http` annotation, so a client generated from
  `gen/openapi/openapi.yaml` will call `GET /api/v1/me` and its siblings, and
  the edge routes the Connect paths and opens exactly one `/api/v1` path, for
  the billing webhook. Opening the REST surface is its own piece of work, with
  a gateway and rate limiting attached to the decision.

- **The machine surface an agentic Watcher needs: what it may read, and the
  one thing it may write** (ENT-258, first of three). The Watcher today is
  three fixed detectors over the compliance profile and the DSAR table; it
  cannot see anything a customer has connected, and it decides nothing. The
  agent version will, and this is the seam it needs: a new internal
  `WatcherService` with `WatcherContext` (the organisation's open profile
  facts, its connections and which of their tools are granted, the signals
  already open, and when the sweep last ran, assembled by core-api in one
  read) and `RaiseSignal`.

  Two properties are worth an operator's attention. **A signal is not a
  finding, and this surface cannot write one**: the Analyst still turns a
  signal into a finding under the citation validator and a human still
  decides, so the separation is enforced by the absence of the RPC rather
  than by a rule. And **no endpoint URL reaches the agent**: it decides what
  to look at, not where to dial, and a fetch still goes through the gateway
  with its egress allow-list.

  No new grant and no migration: the producer role could already read profile
  facts, and read connections and their tools without credentials, and write
  signals. Nothing calls these yet; the skill that will is the next change.

- **The Watcher becomes an agent, and it is off unless you turn it on**
  (ENT-258, second of three). A skill that decides: given one organisation's
  facts, its connections and what has already been raised, it chooses what is
  worth telling somebody about and raises it, one step at a time, with the
  result of each raise feeding the next decision. It is the first skill with a
  tool, and its allow-list holds exactly one: `RaiseSignal`. There is no tool
  that writes a finding anywhere on the surface, so the separation between
  "worth looking at" and "cites the law, and a human decides" is held by the
  absence of the call rather than by a rule in a prompt.

  **`KINDLAST_WATCHER_AGENT=1` on the workers process turns it on, and nothing
  else changes if you do not.** The three deterministic detectors are what runs
  today and they stay: ENT-258 makes them the baseline the agent is compared
  against, and that comparison is the next change and the one that moves this
  default. A deployment with the flag off runs exactly the sweep it ran before,
  and pays nothing: not an activity, not a call.

  Two things worth knowing if you do turn it on. A watch is several model calls
  rather than one, so it is slower than a narrative draft and its activity
  timeout is longer; on a local model, expect a sweep to take minutes per
  organisation. And a signal it raises becomes a finding in the same sweep,
  because the step runs between the detectors and the Analyst.

  No migration and no new grant. The Python service gains one call it may make,
  through an RPC that validates the vocabulary, requires a deduplication key
  and resolves the citation before anything is written.

- **The agentic Watcher runs for everyone, and the comparison against the fixed
  detectors runs in CI** (ENT-258, third of three). The rail now says the
  Watcher is Working rather than Working in part, and what changed is that two
  things run where one ran before: the three fixed detectors are unchanged and
  still go first, and the agent runs after them, is shown what they raised, and
  adds what no fixed rule was written to look for.

  **`KINDLAST_WATCHER_AGENT=0` on the workers process turns the agent off** and
  gives you back exactly the sweep you had before, not a cheaper version of the
  new one. The reason to reach for it is cost rather than safety: a watch is
  several model calls where a narrative draft is one, so a deployment running a
  local model on modest hardware pays minutes per organisation per sweep.

  `scripts/watcher-comparison.py` runs on every change to this repository,
  against a real model on a real stack. It deliberately does not assert that the
  agent covers the detectors, because it is shown their output and told not to
  repeat it. It asserts what must hold whatever the model decided: their signals
  survive it untouched, nothing is written outside the vocabulary or citing an
  obligation the run was not offered, and no finding is written.

- **Fixed: an agent could overwrite a signal a deterministic detector had
  raised** (ENT-258). Signals are deduplicated on `(profile, key)`, so whoever
  writes a key owns the row it lands on, and a watch is shown every open signal
  with its key so that it does not repeat one. Together those meant a model
  that echoed back a key it had been shown did not create a duplicate, it
  rewrote the detector's row.

  Found by the comparison above the first time it ran, on a fixture whose
  "Profile gap: Records of Processing Activities" came back retitled and
  downgraded from high to medium. **Every deduplication key an agent writes is
  now namespaced**, so the collision is impossible rather than checked for. If
  you have run the agent from a pre-release build, its signals will be re-raised
  once under the new keys and the old rows stay open until somebody resolves
  them.

### Changed

- **Approving a finding creates its record a moment later, through the
  Executor workflow, instead of inside the approving transaction** (ENT-271,
  ENT-225 phase 2). Three database triggers used to insert the processing
  activity, DSAR or AI system while the approval was still being written.
  Approving now writes the finding, its audit row and one `executor_jobs` row
  in that transaction (migration 00036), a relay starts one
  `execute/{job id}` workflow per job within fifteen seconds, and the record
  is created **as the person who approved it**, with the same columns and the
  same audit entry the trigger wrote. This is what the design always
  specified: execution belongs behind the event boundary.

  What an operator sees: a record appears in Records a second or two after the
  approval rather than in the same instant; an approval whose record has not
  appeared is a pending row in `executor_jobs` with its attempt count and last
  error, and a workflow in the Temporal UI with the reason in its history. The
  execution retries with backoff and no attempt limit, because a person
  approved a finding and a record is owed.

  **One refusal changes shape, for the better.** Approving a finding that
  would create a High-Risk AI system without ticking the review used to fail
  inside the trigger with a `check_violation`, which reached the caller as an
  internal error. It is now refused before anything is written, with
  `failed_precondition` and the reason, and the finding stays pending so the
  same person can review and approve again.

### Added

- **Findings are narrated on both task queues: Go loads, Python drafts, Go
  persists** (ENT-256, part five, second half; design §16.4, which is now
  built as written). The `intelligence` container runs a Temporal worker
  beside its RPC server, polling the `intelligence` task queue, and every
  sweep's narration step is three activities per finding: `workers` asks
  core-api for the next finding with no narrative and the draft request built
  for it (`NarrativeService.NextFindingToNarrate`), the Python worker drafts
  it (`DraftNarrative`, the same harness, guardrails and run record as the
  RPC of that name, with every model call going back through core-api), and
  `workers` records the narrative or the refusal
  (`NarrativeService.RecordNarrative`). A draft that fails retries as a
  draft, on its own queue, with its own history.

  **Operators:** the `intelligence` container now takes `KINDLAST_TEMPORAL_ADDR`
  (the bundled stack sets it) and `KINDLAST_INTELLIGENCE_CONCURRENCY` (drafts
  in flight at once, default 2, what one local model serves). With no Python
  worker polling, a sweep waits two minutes for a draft to be picked up,
  records narration as skipped with that reason, and completes: an
  `intelligence` container that is down costs explanations, never sweeps.
  `NarrateFindings` remains for an operator who wants to narrate a batch by
  hand.

### Changed

- **Every model call now goes through core-api, and the Python service holds
  no model endpoint and no credential again** (ENT-256, part five, second
  half; the hardening ENT-236 parked). A new internal RPC,
  `CompletionService.Complete`, takes the messages and the organisation;
  core-api resolves whether that organisation uses the deployment's own model
  or a provider it chose, opens the sealed provider key with the key only
  core-api holds, makes the call, and returns the content and usage. The
  Python service asks it for every completion and reads no model URL from its
  configuration. `DraftNarrative`'s `model_endpoint.base_url` and `.api_key`
  are deprecated on the wire, never populated, and **refused** if a caller
  sets them.

  Why: drafts are becoming Temporal activities on a Python worker, and an
  activity's input is written into a workflow history; a key cannot ride
  there, so it no longer rides anywhere. What it costs, said plainly: every
  prompt passes through core-api, which already holds the finding, the
  obligation and the profile it is built from.

  **Operators: `KINDLAST_MODEL_ENDPOINT` is a new core-api setting** (the
  bundled stack sets it to the `model` service; `KINDLAST_MODEL_NAME` joins
  it for the run record), and `KINDLAST_MODEL_URL` on the `intelligence`
  service is no longer read. An organisation's chosen provider (ENT-236)
  keeps working unchanged from the organisation's side; the difference is
  where the key is used.

### Added

- **Findings get their explanation on the sweep, without anybody asking**
  (ENT-256, part five, first half). `NarrateFindings` (ENT-245) was written as
  the job that runs after a sweep over findings that have no narrative, and
  nothing in the product ran it: every finding showed only the deterministic
  text the sweep wrote. It is now the third step of every sweep workflow,
  after the Watcher and the Analyst and, for a triggered sweep, after the
  trigger is settled, so the feed shows the finding the moment it exists and
  the explanation arrives as the model drafts it. One finding per activity,
  up to fifty per run, with a retry policy; a deployment without the `model`
  profile costs one activity per run and nothing is wrong; an organisation
  whose provider cannot be honoured is recorded as skipped in the run's
  result and the sweep is not failed. The daily run narrates one organisation
  at a time after its fan-out, because one local model serves one request at
  a time.

  Not in this change, and why: the design's "Go loads, Python drafts, Go
  persists" as three activities with the draft on an `intelligence` task
  queue served by the Python service. That puts the draft's input into a
  workflow history, and an organisation's own provider key (ENT-236) cannot
  ride in it; the model call therefore stays behind core-api's
  `NarrateFindings`, which opens the key and makes the call. Whether the
  local-model path alone moves to a Python queue is a decision recorded on
  ENT-256 for the maintainers.

- **The Watcher and the Analyst run on a schedule again, and confirming
  onboarding triggers the organisation's first sweep on its own** (ENT-256,
  part four of five; closes the gap ENT-212 left). Since the Supabase schema
  went, nothing ran a sweep unless somebody called `SweepService.RunSweep`
  with a service credential: a member could finish the interview, confirm,
  and land on a dashboard that had no findings and no way to get any. Two
  things change that.

  Confirming writes a row to a new `sweep_triggers` table, in the same
  transaction as the confirmed facts (migration 00035), and the `workers`
  relay starts one `sweep/{trigger id}` Temporal workflow per row within
  fifteen seconds: the Watcher, then the Analyst, then the row marked done.
  The two-step shape is not incidental: `ConfirmProfile` and the sweep run on
  separate connection pools under separate roles (00008), and a synchronous
  call from the first to the second would have raced the transaction that has
  to commit before the facts it wrote are visible to any other connection,
  silently sweeping an empty profile. Writing a durable marker inside the same
  transaction and relaying it afterwards, the same shape `transactional_outbox`
  already uses for invitation mail, closes that race by construction rather
  than by timing.

  And a daily Schedule, `sweep-every-organisation` (06:00 UTC,
  `KINDLAST_SWEEP_SCHEDULE`), lists every organisation with a compliance
  profile and runs the Watcher and then the Analyst over each, four at a
  time, one organisation's failure never stopping the rest; the run's result
  in the Temporal UI says how many were visited and which failed. This is
  what pg_cron's `watcher-daily` and `analyst-daily` were, with the Analyst
  now the next step in the same workflow rather than a second job five
  minutes later. The list comes from `sweep_targets()`, the ninth
  `SECURITY DEFINER` function (agent-only, ids only), because the producer
  role deliberately cannot enumerate tenants; `db/README.md` carries the
  argument.

  Four RPCs join `SweepService` on the internal surface (`RunAnalyst`,
  `ListSweepTriggers`, `SettleSweepTrigger`, `ListSweepTargets`), all on
  `internal:ingest`; `RunSweep` stays for an operator who wants one
  organisation swept now. `docs/self-hosting.md` no longer lists a scheduler
  as a requirement or opens with the silent-nothing failure mode: the
  schedules are inside the stack.

- **Invitation email now leaves through Temporal, one workflow per message**
  (ENT-256, part three of five). The ticker inside core-api that drained the
  transactional outbox every ten seconds is gone. In its place, a
  `relay-transactional-outbox` Schedule (every fifteen seconds,
  `KINDLAST_OUTBOX_RELAY_INTERVAL`) asks core-api what is waiting and starts a
  `DeliverMessageWorkflow` for each row, named `deliver-message/{row id}`, so
  a message is never being delivered twice at once. Each delivery retries with
  backoff (ten seconds, doubling, capped at ten minutes, no attempt limit) and
  every attempt, with what the mail server answered, is in that workflow's
  history. A message that is not leaving is a running workflow in the Temporal
  UI with a reason, rather than a counter in a table.

  What an operator has to know. Mail is still sent by core-api: the worker
  asks it to deliver a message by id through a new internal service,
  `DeliveryService` (`ListUndelivered`, `DeliverMessage`, `ReclaimMessages`,
  all on `internal:ingest`), and core-api claims, sends on its SMTP channel and
  records the outcome in one transaction, as before. So `KINDLAST_SMTP_ADDR`
  stays on core-api, a workflow history never carries an address, a subject or
  a body (the body of an invitation holds the raw token), and a deployment
  without SMTP sees its deliveries retrying with a reason naming the setting,
  and draining on their own once it is set. **`workers` must be running with
  `KINDLAST_TEMPORAL_ADDR` set for invitation mail to leave at all**; a
  gateway-only `workers` says so at boot.

  The retention pass (ENT-242) moves with it: `reclaim-transactional-outbox`
  runs hourly at forty past (`KINDLAST_OUTBOX_RECLAIM_SCHEDULE`) instead of on
  a timer inside core-api, with the same window and the same rule that a
  message which can still be delivered is never touched. It no longer runs at
  core-api boot, because a schedule fires whether or not the process that
  owns it has restarted, which was the reason the boot-time pass existed.

  The outbox table stays. §16.2 says "an activity with a retry policy is the
  outbox", and for the retry half that is what this is; the table remains the
  durable handoff because the message is written in the same transaction as
  the invitation, which a workflow started after the commit cannot promise.

- **Finding notifications inside somebody's quiet hours are now held and
  delivered when the window ends, instead of dropped** (ENT-256, part three,
  second half). A notification is one Temporal workflow,
  `deliver-notification/{row id}`, started by the same relay that starts
  invitation mail. It asks core-api who should hear about the finding and
  when; sends to everybody who is due; sleeps on a durable timer until the
  earliest held recipient's quiet hours end, in their own time zone; asks
  again; and marks the row sent (or skipped, with the reason) when nobody is
  left. A person is told once however many rounds their colleagues take.
  Until now a notification that arrived inside quiet hours was recorded as
  skipped with "inside quiet hours" on the row and never sent, which the code
  documented as the limitation of dispatching on a plain timer.

  Everything else about a notification is unchanged: one unsubscribe link per
  person, one approve link per person with a verified address, the doorbell
  copy that names the finding without quoting it, and the `notification_
  recipients` definer function answering who. The three new internal RPCs
  (`PlanNotification`, `NotifyRecipients`, `SettleNotification`) are on
  `DeliveryService` with the rest. The last in-process delivery timer in
  core-api goes with this; `KINDLAST_APP_BASE_URL` is still required on
  core-api for notifications to leave, and its absence is said at boot and on
  every attempt.

- **Deferred findings come back on their date, on a schedule, which is the
  first thing Temporal runs** (ENT-256, part two of five). Since the Supabase
  schema went, every finding anybody deferred has stayed deferred: the job that
  brought them back was a `pg_cron` entry dropped in migration 00001, and
  nothing replaced it. A Temporal Schedule, `expire-snoozed-findings`, now runs
  hourly at ten past and brings back every finding whose deferral has run out,
  in every organisation at once. Hourly rather than the old daily 06:10 because
  "defer for seven days" should mean seven days, not up to eight.

  The shape is the one every schedule after it will take, and it is worth
  knowing as an operator. `workers` (the integrations gateway's binary, now
  also the Temporal worker on the `core` task queue) registers the schedule
  with the engine at boot; the schedule starts a workflow; the workflow's one
  activity calls a new RPC on core-api's internal surface,
  `SweepService.ExpireSnoozes`, with the same service credential Intelligence
  presents; core-api runs the pass on the producer pool. `workers` still holds
  no database credential. A failed call is retried with backoff and shows up in
  the Temporal UI with its reason; a refused credential is not retried and
  fails the run at once, so it is visible rather than endlessly "trying".

  Two things change in the database. `expire_snoozed_findings()` becomes the
  eighth `SECURITY DEFINER` function, because it is a maintenance pass over
  every organisation started by nothing that has a tenant, which no
  single-organisation policy can express; migration 00034 and `db/README.md`
  carry the argument. And it is executable by the producer role only: it used
  to be PUBLIC, which was harmless while nothing bypassed policies and is not
  now. The application role cannot bring a deferred decision back early.

  **For a self-hoster upgrading:** nothing to run by hand. `workers` now needs
  the authorization server and the seed's client file at boot, which the
  compose file gives it; if you run your own IdP, `workers` takes the same
  `KINDLAST_OIDC_*` and `KINDLAST_INTERNAL_CLIENT_FILE` settings core-api
  does. `workers` now reports not ready until the engine answers, so core-api
  (which waits on it) does not start before the thing its schedules run on.
  The first tick after the upgrade brings back everything that was due while
  nothing ran, which may be a lot of findings at once: that is the backlog
  being cleared, not a fault.

- **Temporal runs in the stack, on its own databases, inside the air gap**
  (ENT-256, build-order step 8, part one of five). Nothing in Kindlast has run
  on a schedule since the Supabase schema went: the three `pg_cron` jobs left
  with migration 00001, the Vercel cron routes left with the old console, and
  between then and now the only thing that has ever produced a finding is
  `SweepService.RunSweep`, called by hand. Temporal is the design's answer
  (§16): every schedule becomes a Temporal Schedule and every hop in the agent
  chain becomes a workflow step, so the domain database does no scheduling and
  carries no scheduler tables.

  This change brings the engine up and nothing else. `temporal` joins a
  default `up`, on `postgres-platform` beside Zitadel and never on the domain
  Postgres, with its own role and its own two databases, provisioned by a
  `temporal-init` job that runs every boot and creates only what is missing.
  The schedules themselves arrive in the next changes, in this order: expiring
  snoozed findings, notification dispatch, then the Watcher and Analyst chain.
  Until the last of those lands a sweep is still started by hand, and
  `docs/self-hosting.md` now says so rather than describing cron routes that
  no longer exist.

  **For a self-hoster upgrading:** nothing to run by hand. The init job
  creates Temporal's role and databases on your existing `postgres-platform`
  volume the first time the new stack starts, which is the reason it is a job
  rather than an initdb script (initdb runs once per volume, and yours already
  has). Two settings are worth reading before you deploy:
  `KINDLAST_TEMPORAL_DB_PASSWORD`, which has a development default, and
  `KINDLAST_TEMPORAL_RETENTION`, which decides how long workflow histories are
  kept and is applied when the namespace is first created. A history carries
  finding ids, so this is a retention decision about personal data; the
  default is seven days and the doc says how to change it on a deployment that
  already exists.

  The Temporal UI is in a `dev` profile and absent from a default `up`, because
  every history it shows carries finding ids and a production stack should
  not publish a browsable view of them on a port nobody asked for. A worktree
  stack gets it an eighth port, `KINDLAST_TEMPORAL_UI_PORT`, from
  `scripts/stack-env.sh` like the other seven.

- **Changes to who can reach a compliance record are now in the audit log.**
  Renaming the organisation, inviting somebody, changing a member's role and
  removing a member each write a row, with what the value was before and what
  it became. Until now every decision about a finding was recorded and every
  change to who was allowed to make one was not, so the log could show that a
  person approved something and not how they came to be in the organisation,
  at what authority, or when it was taken away. That is the first question an
  auditor asks.

  The standard was already set: choosing a hosted model provider writes a row
  (ENT-236) on exactly this reasoning. Membership was the larger gap.

  New `action_type` values, which anything parsing the CSV export should
  expect: `rename_organisation`, `invite_member`, `change_member_role`,
  `remove_member`, `accept_invitation`. An invitation records the address and
  the role offered and never the token, which is a capability and would
  otherwise be re-issued to everybody who can read the log. A rename that
  changes nothing writes no row.

  **Accepting an invitation is recorded too** (ENT-268), which is what closes
  the gap between an offer of access and access actually being taken up. Until
  it was, "invited somebody to join" was the same row whether the invitation was
  still sitting unread in a mailbox or the person had been reading the
  compliance record every day since, and telling those apart is the reason
  access is logged at all. The row names the joiner, points at the invitation it
  redeemed, and carries the role granted. Refusals write nothing, because
  nothing happened: expired, already accepted, never existed and addressed to
  somebody else stay one answer in the log as they are to the caller.

  It arrived a change later than the other four for a structural reason worth
  knowing if you self-host and read the schema. The audit log's insert policy
  binds a row to an active organisation and to a membership for the acting user,
  and somebody redeeming an invitation has neither until the moment they join,
  so the row is written inside `accept_invitation` (a `SECURITY DEFINER`
  function that already existed for the same reason) rather than beside it. The
  policy itself is unchanged: a caller who is not a member still cannot write
  into an organisation's log, and `db/tests` asserts that alongside the new
  behaviour.

  The console names it "Joined by invitation", next to "Invited somebody to
  join", so the offer and the arrival read as a pair rather than as one sentence
  and one column name.

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

### Security

- **An invitation could be redeemed by anybody who held the link, and
  redeeming it granted the role it named.** `accept_invitation` matched on the
  token hash alone and joined whoever was signed in, at the invited role,
  without ever comparing the invitation's address to the caller. An invitation
  to an `owner` seat, forwarded once or opened in a shared mailbox, made a
  stranger an owner of somebody else's compliance record: able to read the
  whole record, approve findings, invite others, export the audit log and send
  the organisation's data to a hosted model provider.

  There was a duller version of the same bug that needed no attacker. The
  person who sent the invitation is usually signed in, so following the link to
  check what the recipient would see consumed it. The actual recipient then got
  a generic "this invitation cannot be used" and had to be invited again.

  Acceptance is now bound to the invited address, matched case-insensitively
  against the caller's verified address as the authorization server states it:
  the `email` claim when the token carries one, and the userinfo endpoint when
  it does not, which is the case on the bundled Zitadel. A mismatch is refused
  **and leaves the invitation unused**, so the intended recipient can still
  accept. Refusals stay indistinguishable from expired, already accepted and
  never existed, so the endpoint still cannot be used to discover which tokens
  are real or who they were for.

  An unverified address is refused too. Without that, somebody could register
  as the invited address, skip the confirmation mail, and walk in.

  **What to check on upgrade.** Existing pending invitations are unaffected and
  keep working for the people they name. If you want to know whether this was
  ever exercised on your deployment, `invitations` records both halves:

  ```sql
  select i.email as addressed_to, u.email as accepted_by, i.accepted_at, i.role
    from invitations i
    join user_identities u on u.user_id = i.accepted_by
   where i.accepted_at is not null
     and lower(u.email) is distinct from lower(i.email);
  ```

  Rows returned are invitations redeemed by somebody other than the addressee.
  Most will be benign, an inviter opening their own link, but each one granted
  a membership, so check them against `memberships` before dismissing them.

### Fixed

- **An invitation that could not be used said nothing at all** (ENT-267).
  Following an invitation link that failed dropped the person into an
  organisation of their own with no message: `/invite/{token}` set
  `/workspace?error=invitation` and nothing anywhere read the parameter. That
  used to be a rare path, reached only by an expired or already redeemed
  token. It stopped being rare when an invitation started being refused for
  anybody except the address it names, which is exactly what happens when the
  person who sent it opens their own link to see what the recipient will see:
  they landed in their own workspace and reasonably concluded the product was
  broken.

  `/workspace` now stops on that parameter instead of resolving onward, says
  the invitation could not be used with the account currently signed in, names
  that account, and offers the organisation it would otherwise have redirected
  to as a link. A sign-in that carried an invitation which failed at the
  callback arrives at the same page rather than silently.

  It deliberately does not say why, and that is a property rather than a
  wording choice: expired, already redeemed, never real and addressed to
  somebody else are one answer from core-api, so that holding a session cannot
  be used to discover which invitations exist. The message keeps that
  distinction unavailable, and a test asserts the absence of the words that
  would give it away. Nothing to do on upgrade, and no invitation behaves
  differently: a refused invitation was, and still is, left unused for its
  actual recipient.

- **Three actions read as column names in the log.** `create_ropa_manual`,
  `create_dsar_manual` and `update_ropa` were written by the backend and absent
  from the console's label table, so an auditor saw the raw value. They now read
  as sentences like every other row.
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

### Security

- **The producer role's reads now name the organisation they are for**
  (ENT-272). `kindlast_agent` is the role the Watcher, the Analyst and the
  evidence ingest path run as. Eight of its policies were `using (true)`:
  `org_profile_facts`, `integrations`, `integration_tools`,
  `integration_fetches`, `audit_evidence`, `org_evidence`, `org_model_config`
  and `agent_runs`. A query on any of them that did not write an `org_id`
  predicate by hand read every organisation's rows.

  That was a deliberate decision when it was made, and it is written down in
  the migrations: the agent runs for organisations nobody is signed in to, so
  it had no tenancy setting to be checked against. It has one now. Every
  producer path that touches these tables knows whose data it is asking for,
  and two of them (recording an agent run, resolving an organisation's model
  endpoint) have been changed to say so rather than relying on a hand-written
  `where` clause.

  The reason to care is not that an attacker could reach these tables. It is
  that a bug pointing the producer at the wrong organisation, or at none, now
  touches no rows instead of every tenant's. That is not hypothetical: the
  first code to read them on this role shipped with another organisation's
  connections and profile facts in the Watcher's context, and a test caught it
  rather than the schema.

  **For self-hosters:** no action, and no data changes. If you have written
  your own code against the `kindlast_agent` role, a read of these eight
  tables must now set `app.current_org_id` first, and will raise
  `unrecognized configuration parameter` rather than returning rows if it does
  not. Policies on the corpus tables and on the cross-tenant relays
  (`transactional_outbox`, `sweep_triggers`, `executor_jobs`,
  `capability_tokens`, the notification dispatch pair) are unchanged, because
  those legitimately list across every tenant.

### Security

- **A signal now records what produced it, and cannot change hands** (ENT-273).
  The Watcher raises signals two ways: deterministic detectors that a person
  can read as rules, and, since ENT-258, an agent. Both write through the same
  function, which deduplicates on `(profile_id, dedup_key)`, so whoever writes
  a key owns the row it lands on. The agent is shown every open signal with
  its key, because a run that is not told what is already open repeats it.

  Those two facts together were a hole. A model that echoes a key back does
  not raise a duplicate, it overwrites the detector's row, and the CI
  comparison caught exactly that on its first real run: a signal reading
  "Profile gap: Records of Processing Activities" was retitled by the model
  and dropped from high severity to medium, in a slot a rule owned, with
  nothing on the row saying so.

  It was closed at the time by namespacing every key the agent writes. That
  held for the one writer that existed, and it was a convention in one function
  rather than a property of the data. `watcher_findings` now carries a
  `source` of `detector` or `agent`, and a trigger refuses any update that
  changes it, so a signal raised by a rule cannot be taken over by a model, or
  the reverse, whatever writes it and whether or not it goes through the
  shared function.

  **For self-hosters:** no action. Existing signals are recorded as `detector`,
  except those the agentic Watcher raised on a development stack, which are
  identified by their key prefix and recorded as `agent`. If you call
  `emit_watcher_finding` yourself, it takes an optional ninth argument and
  defaults to `detector`, so existing calls are unchanged.
- **The Watcher's detectors and the Analyst's conversion are Go, not plpgsql.**
  `watcher_detect_deadlines`, `watcher_detect_gaps`,
  `watcher_detect_dsar_escalation` and `analyst_convert_signal` decided which
  obligation applies to which profile, how urgent a deadline had become and how
  much work a finding was. Those are product judgements that change as
  obligations are added, so by the rule in `db/README.md` they belong in Go, and
  they are now in `apps/core-api/internal/domain/sweep`.

  Nothing a customer sees changes. Every signal and every finding either
  implementation writes is asserted identical, field by field, over a fixture
  set covering gaps, data-subject requests inside and outside both windows,
  overdue requests, sensitive data categories and an organisation with nothing
  to raise. Both halves run as `kindlast_agent` in that comparison, which is
  what makes a read the producer role was never granted fail on the commit that
  adds it rather than on the day a feed goes quiet.

  **One reported number changes, and it was wrong before.** `RunSweep` and
  `RunAnalyst` answer with `signals` and `findings`. The plpgsql returned the
  number of PROFILES it walked, which under the producer role is always one, so
  every sweep has reported `1` and `1` since the endpoint shipped, whatever it
  actually wrote. They now count signals raised and findings converted. Nothing
  branches on either number; they are summed into a workflow history and read by
  an operator, who until now was reading a constant.

  No migration and no schema change. The plpgsql functions are still present and
  still work, deliberately: they are the baseline the Go is compared against, and
  `db/tests/agent-role.test.ts` still calls `run_watcher()` as the producer role.
  Dropping them, re-deriving `kindlast_agent`'s grants from what the Go actually
  reads, and regenerating the grant matrix is the remaining half of ENT-259.
  `kindlast_agent` needs no new grant for this release: the Go reads the same
  five tables the plpgsql did. It records `source = 'detector'` on the signals it
  writes, so ENT-273's "a signal cannot change hands" trigger applies to it the
  same way it applies to `emit_watcher_finding`.

  Citation labels and URLs moved with the Analyst, and the obligation pages
  moved with them in the same change. `analyst_citation_label` and
  `analyst_citation_url` were rendering both a finding's citation and the
  regulation page's, and moving only one would have left two implementations of
  "what is this citation called" free to diverge. There is still exactly one, now
  `corpus.Citation`'s `Label` and `URL`, and its output is asserted against the
  plpgsql over the whole stored corpus.
- **Partner API keys: the API opens to callers that are neither a browser nor
  a person's phone** (ENT-262, part one of three). An owner can mint a key in
  an organisation, hand it to an integration, and revoke it. The credential is
  shown exactly once and stored only as a digest.

  What a key is, because the shape decides everything else: a key **borrows
  the authority of the person who minted it**, in the one organisation it was
  minted in, narrowed to the scopes it was given. It is not an identity, it
  holds no membership of its own, and that person's membership is checked
  again on every request. Offboarding somebody therefore stops their keys with
  them, on the next call, with no sweep to run and nothing for an
  administrator to remember. A key also reads exactly the rows its minter
  reads and is refused exactly where they would be, because as far as Postgres
  is concerned it is them: there is no second policy surface for keys to get
  wrong.

  Three properties worth knowing before you hand one out. **The organisation
  comes from the key and the `Kindlast-Org-Id` header cannot move it**, so one
  credential cannot be redirected at another tenant by whoever sets a header.
  **A key can never mint another key**, because `org:manage` is not a scope a
  key may carry, so a credential cannot extend its own reach with no human
  involved. And **no key can ever hold an `internal:*` scope**, which is a
  CHECK constraint rather than only a Go rule, so it holds against a migration
  and against a psql prompt as well as against the application.

  **Revocation is immediate and it is enforced by `core-api`, not by the
  edge.** The next request presenting a revoked key finds no live row and is
  refused, with no cache to expire and nothing to propagate. Minting and
  revoking are both written to `audit_log`, and neither row contains the
  credential. An act a key performs is attributed to the key rather than to
  the human who minted it, through a new `audit_log.actor_api_key_id` column.

  Keys arrive under their own `Authorization` scheme, `ApiKey`, rather than as
  a second kind of `Bearer`. The two verification paths never fall back to one
  another: a key presented as a bearer token fails as a token, and a token
  presented as a key fails as a key.

  **For self-hosters:** migration `00043` adds `api_keys`, two `SECURITY
  DEFINER` functions used only to authenticate a caller who has no session
  yet, and a nullable `actor_api_key_id` column on `audit_log`. Nothing
  existing changes behaviour and no data is rewritten. Two grants on the new
  table are deliberately column-level, and `db/README.md` explains why:
  `kindlast_app` can list keys and **cannot read a digest**, and the privilege
  that permits revocation cannot also widen a key's scopes. No role holds a
  delete grant on the table, because a revoked key is a record that access
  once existed.

  **Not in this change, and named so nobody assumes otherwise:** the REST
  aliases (`/api/v1/api-keys` and the rest of the annotated surface) are still
  not routed by the edge. Reaching a key-authenticated call today means the
  Connect path under `/kindlast.core.v1.*`, which the edge already routes.
  Opening the REST surface carries a CORS policy, per-key rate limiting and a
  WAF with it, and that is ENT-262's second part. The deprecation and sunset
  policy is its third. There is also no console screen for keys yet, so
  minting one means calling the API; the Postman collection carries the
  requests.

### Fixed

- **A stack could come up with Temporal reporting healthy and its namespace
  missing, which took `workers` down with it** (ENT-275). The healthcheck
  asked `temporal operator cluster health`, which passes as soon as the server
  answers. Registering the `default` namespace is a later step of the same
  boot, so there was a window where compose declared Temporal ready, `workers`
  started against it, and failed on a namespace that did not exist yet. The
  error named `workers`, so the thing that had actually not finished was the
  last place anybody looked.

  Two changes, and either alone would have been enough on a fast machine,
  which is the problem with fixing only one. The healthcheck now describes the
  namespace, which needs the server to answer before it can describe anything
  and so replaces the old check with a strictly stronger one. And `workers`
  waits for the namespace rather than only for the dial: a dial succeeds
  against a server that is up, so returning on it alone was always a guess.

  **For self-hosters:** no action. A first `up` on a slow or busy machine is
  the case this fixes, and if the namespace genuinely never arrives, Temporal
  now stays unhealthy and says so instead of a worker exiting with an error
  about schedules.
### The browser suite is a CI gate (ENT-264)

- **`bun run test:e2e` now runs in CI, against the compose stack and the
  console the stack serves.** It was the one suite in the repository nothing
  ran automatically, and the cost had become visible: several bugs fixed in a
  single week were invisible to every suite CI did run, and visible only in a
  browser against a running stack. It gates every pull request
  (`.github/workflows/ci.yml`, the `e2e` job) and runs again nightly against
  main (`.github/workflows/nightly.yml`).
- **It drives the production console behind the edge, not the development
  server.** The two fail differently, and the containerised build is the one a
  self-hoster runs.
- **The job cannot report green without having tested anything.**
  `scripts/e2e-check.sh` wraps the run: it refuses to start without being told
  which console to drive, it proves the console and the identity provider
  answer before a browser opens, and afterwards it reads Playwright's JSON
  report and fails when the run held no tests, when a spec the job named
  produced none, or when anything was skipped. Playwright exits 0 on the last
  two, so a parked test or a renamed spec file used to be a green tick over
  nothing. The wrapper is usable locally with the same arguments.
- **Three defects in the suite itself, all of which had never run.** Every
  fixture user now lands on onboarding rather than on the dashboard, because of
  the compliance-profile gate, and six assertions still waited for the old URL.
  The invitation fixture passed a `psql` variable through `-c`, which does not
  interpolate, so it was a syntax error every time. And the sign-out test
  demanded a password field where the authorization server shows an account
  picker marked "Signed out", so it failed against a stack where sign-out was
  working correctly.
- **`surfaces.spec.ts` is not in either job yet**, and its own header says why:
  every page it visits sits behind the compliance-profile gate, so a brand-new
  fixture user never reaches one. Unblocking it needs a fixture that arrives
  already profiled.

## [0.1.0]

The version the repository has carried in its manifests since the beginning,
recorded here so the history starts somewhere rather than appearing to begin
mid-sentence. No tag was cut for it, and this file does not attempt to
reconstruct the changes that led to it: the git history is the record for
everything before the first release.

[Unreleased]: https://github.com/Entear-OU/kindlast/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Entear-OU/kindlast/releases/tag/v0.1.0
