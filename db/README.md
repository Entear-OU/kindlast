# The database

Two directories, and they answer different questions.

- `migrations/` is the schema, applied by goose. One file per change,
  numbered, never edited once merged.
- `tests/` is the isolation suite, which asserts the security properties over
  `pg_class` and `pg_policies` rather than trusting that a migration did what
  its comment says.

Read the row level security section of [`AGENTS.md`](../AGENTS.md) before
touching either. This file covers the one rule that is easiest to get wrong in
the other direction.

## If it must hold no matter who writes, it is a constraint. If it decides, it is Go.

That is the whole rule (design doc §14.5, ENT-225). It is worth stating here
because the schema you are looking at does not obey it yet, and the shape of
what is already in `migrations/` is a poor guide to what belongs there now.

`00001` and `00002` brought a large body of plpgsql across from the Supabase
era, where putting business logic in the database was the only place to put it.
Most migrations since have rewritten a function body. That accretion is being
undone one surface at a time, so **a function existing is not a precedent for
adding one**.

### What stays in Postgres

Things that must be true regardless of which process is connected, including a
future one nobody has written yet:

- RLS policies, `FORCE ROW LEVEL SECURITY`, the GUC helpers, the two-GUC form
  and the agent role's documented exception to it
- Check, unique and foreign key constraints
- The append-only trigger on `audit_log`
- Indexes
- The `SECURITY DEFINER` functions that exist because RLS structurally cannot
  express the check. There are seven:

  | Function | Added by | Why it cannot be a policy |
  |---|---|---|
  | `app_org_role` | `00002` | Policies need it, so it cannot itself be gated by one |
  | `app_org_member_count` | `00002` | Same |
  | `accept_invitation` | `00003` | The invitee is not a member yet, so no policy can see their invitation |
  | `notification_recipients` | `00015` | The dispatcher holds no grants on memberships, preferences or identities, deliberately |
  | `redeem_capability_token` | `00015` | The caller has no session, so no tenancy GUC is set and every policy would refuse |
  | `resolve_act_delegation` | `00021` | Same reason as redeeming a capability token: the delegation is what establishes the session, so nothing is set yet for a policy to read |
  | `mint_finding_approval_delegation` | `00027` | The dispatcher mints it and holds no grant on `memberships`, so it cannot check the eligibility the mint depends on |

Each carries its justification in the migration that creates it. A definer
function is how RLS gets bypassed by accident, so adding an eighth means
writing down why none of these seven already covers you.

**This table went stale twice before anyone noticed, which is the argument for
the `Added by` column.** It read "five" while the database held seven:
`resolve_act_delegation` arrived with ENT-230 and `mint_finding_approval_delegation`
with ENT-249, and neither edited this list. The count is checkable in one query,
so check it rather than trusting the prose:

```sql
select p.proname
  from pg_proc p join pg_namespace n on n.oid = p.pronamespace
 where n.nspname = 'public' and p.prosecdef
 order by 1;
```

Two of them arrived together in `00015` and neither was free: the first is
narrowed to the minimum projection delivery needs, and the second answers
identically for every unusable token so it cannot be used to discover which
tokens are real.

The two delegation functions are both expected to move. ENT-225 phase 2 puts
the decision in Go, because who may approve is a decision and minting is a
write: core-api mints, Go decides eligibility, and what stays in Postgres is a
composite foreign key from the delegation onto `memberships (user_id, org_id)`,
so "the delegate is a member of that organisation" becomes a constraint rather
than a function. Their present justification holds only until the dispatcher has
a Go path that mints.

### What belongs in Go

Anything that decides. A rule that consults a plan, a role, a status or a
threshold, and could reasonably be different next quarter, is a decision.

The drivers are not performance. At this scale a Go implementation does the
same row work (§19). They are:

- One language for domain rules, so a reader does not have to hold two
- Table tests, instead of plpgsql that can only be exercised through a live
  stack
- Typed errors. The `check_violation` message-marker parsing that used to live
  in the records store existed only because two business rules lived in SQL,
  and a Go caller had to recover which one had fired by reading English out of
  an exception
- Layer correctness. `00013` moved a billing decision into
  `ropa_manual_activity_limit()` and added a third session GUC to feed it: a
  correct fix in the wrong layer, and the reason that GUC no longer exists

### The test that tells you which one you are writing

Ask what happens if a second process connects tomorrow and does not know about
your rule. If the answer is "the data is wrong and nobody notices", it is an
invariant and belongs in the schema. If the answer is "that process made a
different product decision", it is a decision and belongs in Go.

## Which role holds which command, and where a control is a privilege

Tables start closed. No application role has a default privilege in `public`,
so a table the migrator creates arrives with nothing attached and each
migration grants exactly the commands its table needs. That is the ruling on
ENT-243 and it is the reverse of what 00002 set up, which granted
`kindlast_app` all four DML commands on every table and set a default so every
later table inherited them.

The reason the default had to go is that it made this drift by construction. A
migration writing `grant select, insert on public.new_table` was not narrowing
anything: the four commands were already there and the grant was additive.
Only an explicit `revoke` moved the boundary, so a migration's stated intent
and the role's actual grant were two different things, and `db/README.md`
described the first while the second was in force.

### Why a missing grant and a missing policy are not the same control

Every table has `FORCE ROW LEVEL SECURITY`, so a command with no policy
touches no rows. That looks exactly like a boundary and is not one: it is a
table the application can address and simply finds empty (ENT-210). **A missing
grant fails closed at parse time, where a missing policy fails quietly at run
time.** The loud version is the one worth having, twice over: silence is
indistinguishable from "there is nothing here yet", and a table held closed by
the absence of a policy is one policy away from open, with nothing to fail in
review when somebody adds that policy for a plausible reason.

So when the application should not reach a command, the grant is revoked. The
policy is not asked to carry it alone.

### The controls that are privileges

Each row is a claim, and each claim is a named case in
`db/tests/grant-surface.test.ts`, so re-granting any of these turns a test red
rather than making this table quietly wrong. A row here with no case there is a
control nobody can check.

| Table | Role | Absent | Why | Enforced by |
|---|---|---|---|---|
| `audit_log` | `kindlast_app` | update, delete | The accountability record. Nothing deletes from it and the `audit_log_no_update` trigger refuses a rewrite, so the grant is the layer that fails loudly | `00029` |
| `transactional_outbox` | `kindlast_app` | update, delete | The app enqueues; every drain runs on the agent pool | `00029` |
| `notification_outbox` | `kindlast_app` | insert, update, delete | Enqueued by a trigger on `findings`, which only ever fires as the agent, and marked sent or skipped by the dispatcher | `00029` |
| `findings` | `kindlast_app` | insert, delete | The Analyst authors findings; the customer approves, rejects, snoozes and narrates them, which is update | `00029` |
| `watcher_findings` | `kindlast_app` | insert, update, delete | Signals are the Watcher's output. The app reads them | `00029` |
| `product_review_flags` | `kindlast_app` | update, delete | Append only, and `product_review_flags_no_update` refuses a rewrite at the row level | `00029` |
| `subscriptions` | `kindlast_app` | insert, update, delete | Billing rows are the webhook's to write, on the billing pool | `00029` |
| `user_identities` | `kindlast_app` | delete | Identity rows go with the user by cascade, never by a request | `00029` |
| `notification_preferences` | `kindlast_app` | delete | A preference is turned off, not erased | `00029` |
| `deadline_alert_log` | `kindlast_app` | insert, update, delete | A 00001 leftover with a select policy and no producer. Read only until something writes it | `00029` |
| `weekly_briefing_log` | `kindlast_app` | insert, update, delete | Same | `00029` |
| `goose_db_version` | `kindlast_app` | all | goose's own bookkeeping, owned by the migrator. Not domain data, and swept into `public` by 00002's loop rather than by a decision | `00029` |
| `capability_tokens` | `kindlast_app` | all | Bearer credentials. Only `redeem_capability_token` reaches them | `00015` |
| `billing_webhook_events` | `kindlast_app` | all | Provider payloads the application has no business reading | `00017` |
| `org_profile_facts` | `kindlast_app` | delete, table-level update | Correcting a fact writes a new version and closes the old one, so the app holds `update (valid_to)` and nothing else | `00020`, `00029` |
| `org_evidence` | `kindlast_app` | delete, table-level update | Same shape: `update (superseded_by)` only | `00020` |
| `audit_evidence` | `kindlast_app` | update, delete | Evidence is written once | `00006` |

`kindlast_agent` keeps a blanket update on `findings` deliberately. 00022 left
it with a written reason: `run_analyst()` and the act-path functions also write
that table, and whether they do so as caller or as definer has to be
established before the grant can be narrowed. ENT-225 owns that audit.

`kindlast_migrator` is absent from all of this. It owns the schema and holds
everything on everything, which is what makes migrations work.

### The whole surface

Generated from `information_schema`, not typed by hand, because the failure
ENT-243 documents was not a wrong sentence: it was that a prose claim about
grants could drift from the grants with nothing noticing. A hand-written table
can always contradict the database. One that a test regenerates and compares
cannot.

To refresh it after a migration changes a grant:

```bash
UPDATE_GRANT_MATRIX=1 bun run test:db
```

and commit the diff alongside the migration that caused it. A command shown in
brackets is a column-level grant, and the brackets name every column it covers.

<!-- begin generated grant matrix -->

| Role | Table | Commands |
|---|---|---|
| `kindlast_agent` | `agent_runs` | insert, select |
| `kindlast_agent` | `audit_evidence` | select |
| `kindlast_agent` | `capability_tokens` | insert, select, update |
| `kindlast_agent` | `compliance_profiles` | select, update |
| `kindlast_agent` | `findings` | insert, select, update |
| `kindlast_agent` | `integration_fetches` | insert, select |
| `kindlast_agent` | `integration_tools` | select |
| `kindlast_agent` | `integrations` | select (created_at, display_name, endpoint_url, id, kind, org_id, revoked_at, status) |
| `kindlast_agent` | `notification_outbox` | insert, select, update |
| `kindlast_agent` | `obligations` | select |
| `kindlast_agent` | `org_evidence` | insert, select |
| `kindlast_agent` | `org_profile_facts` | select |
| `kindlast_agent` | `regulatory_article_paragraphs` | select |
| `kindlast_agent` | `regulatory_articles` | select |
| `kindlast_agent` | `regulatory_documents` | select |
| `kindlast_agent` | `regulatory_recitals` | select |
| `kindlast_agent` | `transactional_outbox` | select, update |
| `kindlast_agent` | `watcher_findings` | insert, select, update |
| `kindlast_app` | `act_delegations` | insert, select, update |
| `kindlast_app` | `agent_runs` | select |
| `kindlast_app` | `ai_systems` | delete, insert, select, update |
| `kindlast_app` | `audit_evidence` | insert, select |
| `kindlast_app` | `audit_log` | insert, select |
| `kindlast_app` | `compliance_profiles` | delete, insert, select, update |
| `kindlast_app` | `deadline_alert_log` | select |
| `kindlast_app` | `dsar_trail_entries` | insert, select |
| `kindlast_app` | `dsars` | delete, insert, select, update |
| `kindlast_app` | `findings` | select, update |
| `kindlast_app` | `integration_consents` | insert, select |
| `kindlast_app` | `integration_fetches` | insert, select |
| `kindlast_app` | `integration_tools` | insert, select, update (granted, granted_at, granted_by) |
| `kindlast_app` | `integrations` | insert, select, update (credential_ciphertext, credential_key_id, revoked_at, revoked_by, status) |
| `kindlast_app` | `invitations` | delete, insert, select, update |
| `kindlast_app` | `memberships` | delete, insert, select, update |
| `kindlast_app` | `notification_outbox` | select |
| `kindlast_app` | `notification_preferences` | insert, select, update |
| `kindlast_app` | `obligations` | select |
| `kindlast_app` | `onboarding_messages` | delete, insert, select, update |
| `kindlast_app` | `onboarding_sessions` | delete, insert, select, update |
| `kindlast_app` | `org_evidence` | insert, select, update (superseded_by) |
| `kindlast_app` | `org_profile_facts` | insert, select, update (valid_to) |
| `kindlast_app` | `organisations` | delete, insert, select, update |
| `kindlast_app` | `processing_activities` | delete, insert, select, update |
| `kindlast_app` | `product_review_flags` | insert, select |
| `kindlast_app` | `regulatory_annex_items` | select |
| `kindlast_app` | `regulatory_annexes` | select |
| `kindlast_app` | `regulatory_article_paragraphs` | select |
| `kindlast_app` | `regulatory_article_recitals` | select |
| `kindlast_app` | `regulatory_articles` | select |
| `kindlast_app` | `regulatory_documents` | select |
| `kindlast_app` | `regulatory_enforcement_decisions` | select |
| `kindlast_app` | `regulatory_guidelines` | select |
| `kindlast_app` | `regulatory_recitals` | select |
| `kindlast_app` | `subscriptions` | select |
| `kindlast_app` | `transactional_outbox` | insert, select |
| `kindlast_app` | `user_identities` | insert, select, update |
| `kindlast_app` | `watcher_findings` | select |
| `kindlast_app` | `weekly_briefing_log` | select |
| `kindlast_billing` | `billing_webhook_events` | insert, select |
| `kindlast_billing` | `subscriptions` | insert, select, update |
| `kindlast_ingest` | `obligations` | insert, select, update |
| `kindlast_ingest` | `regulatory_annex_items` | insert, select, update |
| `kindlast_ingest` | `regulatory_annexes` | insert, select, update |
| `kindlast_ingest` | `regulatory_article_paragraphs` | insert, select, update |
| `kindlast_ingest` | `regulatory_article_recitals` | insert, select |
| `kindlast_ingest` | `regulatory_articles` | insert, select, update |
| `kindlast_ingest` | `regulatory_documents` | insert, select, update |
| `kindlast_ingest` | `regulatory_enforcement_decisions` | insert, select, update |
| `kindlast_ingest` | `regulatory_guidelines` | insert, select, update |
| `kindlast_ingest` | `regulatory_recitals` | insert, select, update |

<!-- end generated grant matrix -->

## Running the suite

```bash
bun run test:db
```

It needs the compose stack. **It self-skips when the stack is unreachable**, so
a green local run does not prove it ran; check the test count. CI boots the
stack and fails loudly if it cannot, which is what stops coverage disappearing
silently.

## `audit_log` has no retention policy, and that is a decision

Nothing deletes from `audit_log`. Not a scheduled job, not a cascade, not a
manual path in the product. The table carries an append-only trigger, has no
delete policy, and `kindlast_app` holds neither a delete nor an update grant on
it, so the absence is enforced rather than merely observed.

This is written down because ENT-223 asked for it to be a decision rather than
an omission, and because from the outside the two look identical.

**That sentence was false from the day it was written, and how it was false is
why [Which role holds which command](#which-role-holds-which-command-and-where-a-control-is-a-privilege)
exists.** It credited the missing delete grant, and the grant was present:
`00002` gave `kindlast_app` all four commands on every table, so what actually
refused a delete was the absent policy. The property held the whole time, one
layer thinner than this file claimed, and the layer holding it was the one that
fails silently. `00029` (ENT-243) revoked the grant, which is what made the
sentence true rather than what made the log safe. It is checked by name in
`db/tests/grant-surface.test.ts` now, so it cannot go quietly false again.

**Why keep everything.** The value of the record is that a regulator can be
shown it, and a record that thins out after a fixed window is one whose answer
to "what happened in 2024" depends on when somebody asks. Accountability under
Article 5(2) has no expiry, enforcement limitation periods run for years, and
the specific thing a customer buys here is that nobody, including us, can
quietly make a decision disappear. A retention job is a supported, audited,
scheduled deletion path into the one table that must not have one.

**What it costs.** An organisation making twenty decisions a day writes roughly
seven thousand rows a year, each a few hundred bytes plus two jsonb payloads.
That is not a table which grows into a problem at any customer size this
product is aimed at, and every read is keyset over
`(org_id, occurred_at desc)`.

**What would change the answer.** A customer with a contractual or statutory
requirement to delete after a fixed period, which some sectors do have. That is
a per-tenant policy with an audit trail of its own rather than a default, and
it is a different piece of work from adding a cron. If it is ever built, the
deletion itself has to be recorded somewhere that is not the table being
deleted from.

**The other half of this decision is
[`docs/backup-and-restore.md`](../docs/backup-and-restore.md), and the two
belong together.** Everything above is about refusing to delete the record. A
dead volume produces exactly the outcome that refusal exists to prevent, so a
schema this careful about retention and silent about loss would be rigorous in
one direction only. The runbook covers what is irreplaceable, why
`postgres-app` and `postgres-platform` are one backup unit rather than two, and
the verification that matters here: a restore is checked by asserting RLS is
still enabled and forced on every table, not by counting rows, because a
restore that silently dropped row level security would look correct in a row
count and be a data breach. Measured on 2026-08-18, it does not: 43 tables, 43
forced, 136 policies and the append-only trigger all survive a dump and
restore.

One open question, deliberately not answered here: `before` and `after` hold
whatever the acted-on row contained, which for a DSAR includes a data subject's
name. An erasure request reaching those payloads needs a decision about whether
an accountability record is exempt under Article 17(3), which is a legal call
rather than an engineering one. Raise it; do not solve it in a migration.
