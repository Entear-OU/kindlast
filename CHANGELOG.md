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

### Changed

- **A deployment can point at a model it runs itself, without the bundled one**
  (ENT-282). `KINDLAST_MODEL_ENDPOINT` is now overridable. Set it in
  `deploy/.env` to any OpenAI-compatible endpoint (a llama.cpp, vLLM or Ollama
  on the host, on your LAN, or a hosted endpoint that needs no key) and leave
  the `model` profile down:

  ```bash
  KINDLAST_MODEL_ENDPOINT=http://host.docker.internal:8080
  ```

  `docs/self-hosting.md` has told self-hosters to do this for some time, and it
  was not possible: the value was hardcoded in `deploy/compose.yaml`, so the
  only way to follow the instruction was to edit a tracked file and carry that
  edit across every upgrade. The default is unchanged, so a stack that sets
  nothing behaves exactly as before.

  This does not loosen the per-organisation providers, which still require
  HTTPS and still refuse a host resolving to a private address. That asymmetry
  is deliberate: this setting is an operator configuring their own deployment,
  and that one is a tenant naming a host core-api will dial on their behalf.

- **Intelligence no longer needs the bundled model to run** (ENT-282). It has
  sat behind the `model` profile with `depends_on: model`, which described the
  architecture ENT-256 part five replaced: it holds no model endpoint and no
  credential now, and asks core-api's `CompletionService` for every completion.
  The effect was that no agent surface (the Analyst, narration, the console's
  assistant) could run without also running a multi-gigabyte model container,
  even where every organisation used a provider it had chosen. `intelligence`
  is a default service now, so `docker compose up -d` starts it and
  `--profile model` still adds only `model` and `model-init`.

  **If you are upgrading**, `KINDLAST_INTELLIGENCE_URL` now defaults to
  `http://intelligence:8090` rather than empty, because the container is always
  present. If you added that line to `deploy/.env` when enabling the model
  profile, you can drop it; leaving it changes nothing. To run without
  Intelligence at all, set it empty explicitly.

### Fixed

- **The self-hosting guide now says the model profile needs
  `KINDLAST_INTELLIGENCE_URL`.** The compose file has always defaulted it to
  empty on purpose (a profile cannot set a variable on a service outside it,
  and empty is the honest no-model report), and its comment pointed at
  instructions that did not exist. A stack brought up with `--profile model`
  but without the line ran its sweeps and notification drafts normally while
  every surface a person waits on, asking the Analyst, asking the Hands,
  narrated findings, reported "this deployment runs no model". If that
  describes your deployment, add `KINDLAST_INTELLIGENCE_URL=http://intelligence:8090`
  to `deploy/.env` and recreate `core-api`.

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
- **Onboarding is the readiness assessment now, and `/readiness` is gone**
  (ENT-254). Two question flows existed and overlapped: thirteen tapped
  questions on the public marketing page, which asked good questions and
  recorded nothing, and about eleven typed ones inside the product, which fed
  the Watcher. Asking a customer both was the problem. There is one set now,
  eleven questions, every answer a tap, and it lives at
  `/o/{slug}/onboarding` behind an account.

  **Answers are saved as they are given.** The confirmation step is gone: a
  fact is written the moment its question is answered, with `source =
  'onboarding'`, through the same close-then-insert path a correction takes, so
  it is visible and correctable on the memory page immediately. The
  `compliance_profiles` row the Watcher reads is still written once, by the
  answer that finishes the interview, so a half-finished interview produces no
  findings and every authenticated route goes on routing that person back into
  it. Skipping every question finishes nothing and writes no profile, because a
  profile of defaults nobody stated is worse than no profile.

  **The corpus is on screen while you answer.** The fifteen obligations
  Kindlast holds are beside the questions and each one resolves as the answer
  that decides it arrives, in three states: matched, set aside, and still open.
  Every statement of law shown is a corpus row quoted byte for byte, with its
  citation and a link to the official text; everything written about your
  organisation is written from your own answers and asserts nothing legal.

  **What a self-hoster and an integrator have to know.** `/readiness` now
  returns 404 and is out of the sitemap; the marketing site's calls to action
  point at sign-up, with copy that no longer promises an account-free
  assessment. `AnswerQuestion` writes the fact, so a client no longer has to
  call `ConfirmProfile`; that RPC still exists, still works, and now finishes
  an interview early rather than being the only fact writer, and confirming an
  already-completed session enqueues no second sweep. Every list question
  carries `options`, a closed set of tokens the answer must come from, and a
  `basis` naming the corpus obligation to quote; an answer outside the set is
  refused with a sentence naming what was offered. Six questions were dropped
  and none of their fact keys were removed from the vocabulary, so anything
  already recorded is untouched and all of it is still writable from the memory
  page: `industry`, `data_subjects`, `eu_jurisdictions` and `staff_count`,
  which nothing reads, and the readiness page's two questions about a written
  subject-access process and a breach plan, which have no fact key to be
  recorded under. Four questions were added, all of them read by the Watcher
  and none of them previously asked: lawful bases, high-risk processing,
  large-scale monitoring, and whether any AI falls inside the AI Act's
  high-risk list.

  **An organisation that finished onboarding before this upgrade is
  unaffected.** Its facts, its profile and its findings stand, and the
  questions it was never asked can be answered on the memory page.

### Changed

- **The Watcher is told which signals a rule raised** (ENT-276). Every sweep
  shows the agent what is already open, with the key each signal is stored
  under, because a run that is not told what is open repeats it. Those keys are
  also addresses: the schema deduplicates on them, so writing one lands on
  whatever row already holds it.

  ENT-273 made that safe, by refusing any attempt to take over a signal a
  deterministic rule raised. What it could not do is make the constraint
  visible to the thing it applies to. A model shown a list of addresses, with
  no indication that some are not its to write, will reasonably try one, be
  refused, and end the run: the organisation gets nothing from that sweep, for
  a reason nobody can act on.

  Signals a rule raised now say so, in words, in the context the model reads.
  The refusal is unchanged and still lives in the database, which is where the
  authority belongs; this only makes it predictable.

- **You can ask the Analyst why a finding applies to you** (ENT-270). The agent
  rail has ended in a card promising chat, call and a walkthrough since the
  console shell was built, over three icons that did nothing. The first of the
  three is real: on any finding, a box asks the Analyst a question about that
  finding, and the answer comes back with the record of the run that produced
  it, so "how this was produced" is something to read rather than a heading
  over nothing.

  It answers about your organisation and refuses to state the law, which is a
  deliberate limit rather than a gap. The provision is quoted on the same page,
  from the corpus, written by a person, and a small model asked to restate it
  gets it wrong in ways a reader checking the citation cannot see. An answer
  that states the law anyway is refused, and so is one citing anything but the
  obligation the finding was raised against, whatever the question asked for.

  A refusal is drawn as a refusal rather than as a fault, because it is the
  guardrail working, and it carries its run so you can check it.

  **Operators:** nothing new to configure. A deployment running without the
  model profile says so in the panel and nothing about the finding is missing
  without it. The new RPC is `ConversationService.AskAboutFinding`, bound at
  `POST /api/v1/findings/{finding_id}:ask` and carrying a new human scope,
  `agents:ask`, which every signed-in member holds and which no identity
  provider has to grant. It is separate from `findings:read` because reading a
  finding and running a model over it are separately dangerous: one discloses,
  the other spends a budget and sends your own words to whichever provider you
  chose.

  Call and Walkthrough now say they are not built yet, in the same words the
  agents page uses for the Messenger and the Hands, rather than sharing one
  sentence with something that works.
- **Telegram is a second notification channel, on the one dispatch path**
  (ENT-263). A member can link a Telegram chat in notification settings, prove
  they hold it with a code that arrives there, and have finding notifications
  delivered to it instead of by email.

  What matters for anybody operating this is what did **not** happen. There is
  no second queue, no second retry policy and no second answer to "did this go
  out". `transactional_outbox`, `notification_outbox` and the Temporal relay
  are unchanged; Telegram is an adapter behind the delivery seam core-api has
  held since the outbox was built, chosen by a channel name the row already
  carries. Quiet hours and the severity floor are untouched and still apply per
  person regardless of channel, because they say when somebody wants to be
  interrupted rather than how.

  **`KINDLAST_TELEGRAM_BOT_TOKEN` on core-api is the whole of the
  configuration, and leaving it unset means the channel is not offered rather
  than offered and broken.** With no token there is no adapter: the settings
  page reports Telegram unavailable with a reason, linking a chat is refused
  before anything is written, and nothing in the process can reach
  `api.telegram.org`. Existing deployments therefore need to do nothing, and
  `bun run test:airgap` is unaffected. `KINDLAST_TELEGRAM_BOT_TOKEN_FILE` is
  preferred over the variable, because Telegram puts the token in the URL path
  of every API call and it leaks through anything that records a request.

  The token is per deployment, not per organisation. It is not in the database,
  not in `web` and not in the Python service, so it is in no backup of the
  domain schema.

  An unverified chat is never delivered to. The notification goes to the
  person's email instead with the reason recorded, and the same holds after
  they unlink: future messages go to the remaining channel or nowhere, never to
  the chat that was removed.

  **There is no inbound path, deliberately.** core-api registers no webhook and
  polls for nothing, so nothing typed into a chat enters the product. That is
  why linking asks a person for their own chat id rather than having them
  message the bot: a chat message is data and never instruction, and reading
  one means owing a full answer about where that text may flow. Reading replies
  arrives with the Messenger, with that answer.

  Migration `00044` adds `notification_channels`, a `finding_channel` column on
  `notification_preferences` defaulting to `email`, and a channel and chat
  recipient on `transactional_outbox`. Every existing row keeps behaving
  identically. The retention pass now clears a chat id alongside an address,
  and abandons an undelivered verification code after an hour rather than
  retrying one that can no longer be used.

### The Hands: what approving a finding will do, and the record it prepares

- **A third agent, and the first whose job is to not decide** (ENT-261).

  Approving a finding whose action is `create_ropa` or `create_ai_system`
  creates an entry in a register. Until now that entry arrived saying "Not
  recorded" in every column and marked "Needs review", which is correct and
  useless: the product knew the organisation's industry, its data categories
  and its vendors, and put none of it in the record the approval created.

  The Hands is the skill that closes that. Given one finding, it explains in
  the organisation's own terms what approving will do, which register gains an
  entry and what it will say, fills the columns the organisation's recorded
  facts support, and says which columns it left and why. A column it could not
  fill is listed with a reason in the second person, not omitted: a plan that
  is silent about a column reads as a plan that finished it.

  **Every value names the fact it came from, and the name is checked twice.**
  A prepared value is a claim about where it came from, and this product's
  worth is that a human can check a claim. So `from_fact` is required, the
  harness refuses a key the run was not shown, and core-api refuses a key the
  organisation does not hold. Those refuse different things: the first catches
  a model producing a key from somewhere other than its context, the second
  catches a key that is not a fact at all. A record filled with plausible
  values and no provenance would have been worse than the empty one it
  replaces.

- **It cannot approve, and that is structural rather than instructed.**

  The skill's tool allow-list holds exactly one entry,
  `HandsService.PrepareRecord`, which writes a proposal onto a finding. There
  is no RPC it can reach that approves: approving is `findings:act`, which only
  a signed-in person's token carries, and it reads the approver from the
  session rather than from a request field. There is no RPC it can reach that
  creates a register entry: that is `ExecutorService.ExecuteJob`, acting on an
  `executor_jobs` row that is written in exactly one place, inside the
  transaction that records a human's approval.

  A run asking for anything else is refused against the allow-list, the refusal
  is written into `agent_runs` where a customer can read it, and the run ends
  rather than being allowed to try again. The grammar deliberately permits the
  model to ASK for `approve_finding`, because a schema that made the request
  inexpressible would hide that it was wanted and leave nothing in the record.

  A plan is also refused once the approval it was meant to inform has been
  enqueued. After that moment the payload is the Executor's input, and a run
  arriving late must not rewrite what somebody approved.

- **A run that could not start still leaves a record.** The offered field and
  fact sets are read defensively, because they are built before the runner's
  error handling and anything they raised would have escaped with the whole
  call, leaving no `agent_runs` row for a run that really happened. That is the
  failure ENT-277 was filed for, and it is the one outcome the harness must
  never produce.

- **For self-hosters.** Two new internal RPCs, `HandsService.ExplainApproval`
  and `HandsService.PrepareRecord`, both on `internal:ingest`, and
  `IntelligenceService.ExplainApproval` on `internal:intelligence`. No schema
  change and no migration: the plan and the proposed payload live in
  `findings.metadata`, which already existed. A deployment running no
  Intelligence answers `failed_precondition` with a reason rather than 404, so
  the surface is present and honest about being unusable, and every finding
  still carries exactly what it carried before.
- **The Watcher can look at what your connected systems reported, not only at
  the fact that you connected them** (ENT-274). Until now the agentic Watcher
  was shown which systems an organisation had connected and which of their
  tools were granted, and had no way to read any of it: it decided from
  onboarding answers and connection metadata. It now has a second tool,
  `read_evidence`, which returns the observations a fetch already deposited for
  one connection and one of its granted tools.

  **It reaches nothing.** Nothing about this puts a packet on your network. A
  Watcher run reads rows that a fetch through the policy gateway had already
  stored, redacted before they were written; the live call is still the
  Integrations page's "fetch now", which a person starts and waits for. A
  sweep calling a slow endpoint on its own initiative is a different shape of
  change and is not this one.

  **A tool you have not granted is refused, and the refusal is in the record.**
  The grant is checked in core-api against `integration_tools` as well as in
  the harness, and withdrawing a grant stops an agent reading what that tool
  deposited even though the rows remain. A refused read appears in
  `agent_runs.tool_calls` marked refused, with the reason, so "we did not look
  at that because you have not granted it" is a sentence you can read back.

  **What comes back is data and never instruction.** It reaches the model in a
  user turn, fenced and labelled, and never in a system prompt. Each run may
  read at most three times and each read is capped in size, so a system that
  returns a great deal of text cannot fill a run's context with it.

  New RPC `WatcherService.ReadEvidence` on the internal surface, requiring
  `internal:ingest`, which is issued to service principals and never to a
  browser. No migration and no new grant: it reads through the producer role's
  existing privileges. The Watcher skill version moves to `1.1.0`, so runs
  recorded before and after this are distinguishable in `agent_runs`.

- **The Messenger exists as a skill: it drafts the words of a notification,
  and it can only hand them to the dispatch path** (ENT-260). The fourth of
  the four agents. Given the shape of one finding notification (the
  organisation's name, how serious it is, how much else is open, which
  channels it goes out on), it writes the subject and opening prose a person
  would read, in place of the fixed template's one-size sentence.

  **Nothing sends it, and nothing about your deliveries changes yet.** No
  caller is wired in this release: the dispatch path still renders the
  template, so every message you receive is the same one as before. The agents
  page says exactly that, and the Messenger's status reads "Working, in part"
  rather than "Working" until a message you receive has actually been through
  it.

  **It cannot send, structurally rather than by instruction.** Its allow-list
  holds one tool, `queue_message`, which hands a draft over and nothing else;
  the Python service holds no mail or chat credential and no way to obtain
  one; and who a notification reaches, on which verified channel and when, is
  decided by the same delivery code as before, which the draft cannot touch. A
  model that asks to send is refused in code, and the refusal is written into
  `agent_runs` for you to read.

  **A draft is refused rather than repaired** when it contains anything that
  reads as a link, an address or a phone number (every link in a notification
  is minted per recipient by the server, so a written one is one nobody
  minted), when it states what the law requires, or when it uses typography
  the house style forbids. A refused draft is withheld entirely and the
  template is used, so the failure mode is the message you already get today.

  **It is deliberately not told what the finding says.** A notification says
  that something needs a decision and how urgent it is, never what was found:
  that stays behind your sign-in, which has been the rule since notifications
  shipped. The Messenger cannot restate what it was never shown, and the
  request shape has no field that could carry it.

  New RPC `IntelligenceService.DraftMessage` on the internal surface,
  requiring `internal:intelligence`, plus a `DraftMessage` Temporal activity
  on the `intelligence` task queue. No migration, no new scope, no schema
  change. The skill is `messenger.draft` at `1.0.0`, recorded on every run.

- Connected systems are now fetched on a schedule, so the Watcher has evidence
  to read without anybody clicking Fetch. A new Temporal schedule,
  `fetch-evidence-for-every-connection`, asks core-api hourly which granted
  read-only tools have no fetch attempt in the last day and runs one fetch per
  connection and tool, through the same gateway, egress allow-list, tool
  policy and rate limit a manual fetch goes through. Each fetch runs on the
  standing consent of the person who connected the system, and stops when that
  person leaves the organisation. Outcomes are recorded in
  `integration_fetches` whatever they are, including refusals and a customer's
  endpoint being down, and an endpoint that returns the same bytes as last
  time has its fetch linked to the existing observation rather than writing an
  identical `org_evidence` row every day.

  For an operator: migration `00048` adds two `SECURITY DEFINER` functions
  (`fetch_targets`, `integration_fetch_context`) and an index, and widens no
  role, in particular not the producer role's deliberately credential-less
  select on `integrations`. New RPCs `FetchService.ListFetchTargets` and
  `FetchService.RunScheduledFetch` on the internal surface require
  `internal:ingest` and are served only when a gateway is configured. New
  setting `KINDLAST_FETCH_RELAY_INTERVAL` (default `1h`) controls how often
  staleness is checked; how often a customer is dialled (at most daily per
  tool) is a server constant. Pausing the schedule in Temporal is the off
  switch. Agents still cannot cause a fetch; whether they ever can is a
  separate decision this change deliberately does not take.
- **The Watcher can ask for a fetch, and only ask** (ENT-279). The agentic
  Watcher gains a third tool, `request_fetch`: it may request that one granted
  read-only tool on one connection be fetched again, when what is stored is
  missing or too old to raise a signal on. The shape is mediated end to end,
  and the mediation is the feature: the agent asks, core-api decides, and the
  fetch runs later through the policy gateway, behind the egress allow-list,
  under the standing consent of the person who connected the system. The
  answer the agent gets is an acknowledgement, never a payload, so no run is
  answered with your live systems and the run that asked reads the result, if
  any, only through `read_evidence` on a later sweep.

  **No role was widened.** The producer role that runs models keeps the
  column-limited select on `integrations` that omits the sealed credential; an
  ask is a `fetch_requests` row and nothing else, and everything that touches
  a credential or a network stays on roles and processes the agent cannot
  reach. A tool you have not granted, a tool that can write, and a revoked
  connection are each refused by core-api against your own rows, whatever the
  model believed, and the refusal lands in `agent_runs.tool_calls` where you
  can read it back.

  **How often your systems can be dialled because a model asked is bounded
  twice, and neither bound is the model's to move.** Each run may ask at most
  twice, below its three reads. And core-api holds a one-hour cooldown per
  connection and tool: a pair fetched or even attempted inside it is answered
  from the record, and a request already waiting is not queued again, so
  repeated asks, including a sweep's automatic retries, cause at most one
  fetch per pair per hour on top of the daily schedule.

  New RPC `WatcherService.RequestFetch` on the internal surface, requiring
  `internal:ingest`. Migration `00049` adds `fetch_requests`, closed like
  every table: the producer role can insert and read its own organisation's
  asks and cannot rewrite or remove one. The Watcher skill version moves to
  `1.2.0`. Queued requests are served by the scheduled fetch relay, which
  lands separately as the other half of ENT-279; until it does, a queued ask
  waits and everything the customer sees stays truthful about that.
### Changed

- **The words in a finding notification are the Messenger's** (ENT-280). The
  doorbell workflow now runs the Messenger between deciding who to tell and
  telling them: the plan carries a drafting instruction built from rows
  core-api already holds, the draft happens on the Intelligence worker under
  the budgets and critics ENT-260 shipped, and the delivery transaction opens
  the message with the drafted words while still minting every link per
  recipient.

  What did not change is who is in charge. The Messenger cannot stop a
  doorbell: a draft that fails, is refused by its own guardrails, or arrives
  carrying a link falls back to the template, and the message leaves either
  way. The drafted words are checked a second time beside the send, because
  they rode through a workflow history and a second service on the way, and
  under our From: header a link a model wrote is a phishing primitive. The
  structural half of §17.1 also holds at the wire: the drafting instruction
  cannot carry what the finding says, pinned by tests on both sides.

  **For self-hosters:** no action. A deployment without the model profile
  keeps sending the templated words, now via the explicit fallback rather
  than by being the only path.

## [0.1.0]

The version the repository has carried in its manifests since the beginning,
recorded here so the history starts somewhere rather than appearing to begin
mid-sentence. No tag was cut for it, and this file does not attempt to
reconstruct the changes that led to it: the git history is the record for
everything before the first release.

[Unreleased]: https://github.com/Entear-OU/kindlast/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Entear-OU/kindlast/releases/tag/v0.1.0
