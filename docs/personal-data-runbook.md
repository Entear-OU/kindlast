# Where personal data lives, and how to answer a request about it

An operator's runbook for **Kindlast as a processor of its own users' data**
(design doc §14.3, ENT-234). It answers: if somebody asks what Kindlast holds
about them, or asks for it to be erased, where do you look and in what order do
you act.

## What this is not

It is not the customer's DSAR register. That is a product surface, it lives at
`/o/{slug}/records`, and the trail a customer's response gets built from is
`dsar_trail_entries` (ENT-226, migration 00024), readable at
`/o/{slug}/records/dsars/{id}`.

The difference matters because the two answer different questions. A customer's
DSAR concerns their data subjects and their own systems. This document concerns
the people who sign into Kindlast, and the handful of third parties whose
details end up here as a side effect. Everything below is about our stores.

## The stores

| Store | Holds | Durable |
|---|---|---|
| `postgres-app` | The domain tables. Everything in the next section | Yes |
| `postgres-platform` | Zitadel: identity, credentials, sessions | Yes |
| Redis | `web` sessions, and core-api limits | **Yes, see below** |

Two things about the edges of that table.

**`postgres-platform` holds two databases today, `postgres` and `zitadel`.**
The design anticipates `temporal` and `temporal_visibility` alongside them
(`compose.yaml:22`), and workflow histories will hold personal data when they
arrive. They are expected at build-order step 8 and their absence now is
correct, not an oversight. Revisit this section then.

**Redis is not as disposable as it looks.** It runs with `appendonly yes` and
an RDB `save` policy, so sessions survive a restart of the container. A session
is a person's `user_id` and their tokens. It therefore belongs in the erasure
path, which is not the conclusion you would reach from "it is just a cache".

Langfuse, when it lands, joins this table with traces. Redacted at the SDK,
short retention, deletable (companion §7.2).

## The tables, and which role can reach each

Every table below is in `postgres-app`, under `FORCE ROW LEVEL SECURITY` with
policies in the two-GUC form. The role column is what an operator needs when
reasoning about who could have touched something.

| Table | The personal data in it | Reachable by |
|---|---|---|
| `user_identities` | `email`, `display_name`, `issuer`, `subject` | `kindlast_app` |
| `memberships` | `user_id` and the role held | `kindlast_app` |
| `invitations` | `email`, `token_hash` | `kindlast_app` |
| `audit_log` | `user_id`, `approving_user_id`, `actor_role`, and the `before` / `after` jsonb | `kindlast_app` (no delete grant) |
| `dsars` | **`subject_name`**, `handler`, `created_by` | `kindlast_app` |
| `dsar_trail_entries` | **`detail`**, free text a handler wrote about a data subject, plus `source` and `created_by` | `kindlast_app` (select and insert only) |
| `onboarding_messages` | `content`, free text written by a person, plus `created_by` | `kindlast_app` |
| `onboarding_sessions` | `created_by` | `kindlast_app` |
| `compliance_profiles` | `data_subjects`, `created_by` | `kindlast_app`, `kindlast_agent` (select, update) |
| `notification_preferences` | `email`, a contact address separate from the sign-in one | `kindlast_app` |
| `transactional_outbox` | `recipient_email`, `subject`, `body_text`, `body_html`, **until redacted**; see the retention section below | `kindlast_app` (select, insert), `kindlast_agent` (select, update) |
| `notification_outbox`, `deadline_alert_log`, `weekly_briefing_log` | `user_id` | `kindlast_app` (+ `kindlast_agent` on the first) |
| `findings`, `processing_activities`, `ai_systems`, `product_review_flags` | `created_by`, `approved_by` | `kindlast_app` (+ `kindlast_agent` on `findings`) |
| `capability_tokens` | `user_id`, `token_hash` | `kindlast_agent` |

The other three roles hold nothing relevant here. `kindlast_billing` reaches
only `subscriptions` and `billing_webhook_events`. `kindlast_ingest` reaches
only the regulatory corpus, which holds no personal data.

`kindlast_vector_ro` holds **no grants at all, anywhere**, and that is
deliberate rather than unfinished: it is the read-only role for Intelligence's
vector search, and `deploy/postgres/init/01-roles.sh:87` and `compose.yaml:14`
both say it is waiting for the chunk tables (§14.1 item 5).

Two details matter for anyone auditing this later. **The `vector` extension is
installed** (0.8.6) and **no column anywhere uses it**, so the capability is
provisioned and unused. And the chunk tables it waits for are not merely
unbuilt: ENT-228 makes per-organisation embeddings conditional, "only if a
consumer needs them". So the honest status is dormant pending a decision that
has not been taken, rather than dormant pending an implementation that is
scheduled. If that decision lands as "no embeddings", the role and the
extension should both go, and this section should say so.

`kindlast_migrator` reaches everything and bypasses RLS. It is the role an
operator uses for the procedures below, and the only one that can.

### Two categories that surprise people

**Personal data about people who are not users.** `invitations.email` is the
address of somebody who may never have accepted, and `dsars.subject_name` is a
data subject named by a customer, who has no relationship with Kindlast at all.
`dsar_trail_entries.detail` is about that same person: it is meant to describe
what was found rather than to hold it ("employment record, 2019-2024", not the
record), and nothing enforces that, so treat it as though it does hold it.
Neither has an account. So "delete my account" does not reach them, and a
request *from* one of them cannot be answered by finding their user record,
because there is not one. Search by value, not by `user_id`.

**Message bodies that were never meant to persist.** `transactional_outbox`
holds `recipient_email`, `subject` and both bodies, and for the only kind that
exists today the body contains the invitation's accept link, which contains the
raw token in a path segment. It used to hold all of that forever: rows moved to
`status = 'sent'` and nothing removed them, so what accumulated was the address
and the full text of every invitation ever sent, including to people who never
accepted and have no account to be erased with.

**That is now answered rather than open (ENT-242, migration 00030), and the
answer is redaction rather than deletion.** The table has its own section below,
because for anyone answering an access or erasure request the useful facts are
what survives and what does not.

## Two things the database will not enforce for you

Both were verified against the running schema. Get either wrong and nothing
raises an error, which is what makes them worth a section.

### 1. Nothing has a foreign key to `user_identities`

There are **zero** foreign key constraints referencing `user_identities`.
Every `user_id`, `created_by` and `approved_by` in the schema is a bare `uuid`.

That is deliberate. Identity is Zitadel's, and the domain schema mirrors rather
than owns it. But the consequence for erasure is sharp: **delete a
`user_identities` row first and you silently orphan every reference to that
person**, in `audit_log`, in `findings.approved_by`, in every `created_by`. No
cascade fires. No constraint complains. The rows simply point at nothing.

So the order in the erasure procedure is not a stylistic preference. It is the
only thing standing between you and quiet corruption.

### 2. The append-only guarantee covers UPDATE, not DELETE

This now applies to three tables, not one: `audit_log` (00001), `agent_runs`
(00019) and `dsar_trail_entries` (00024). All three carry a `BEFORE UPDATE`
trigger that refuses even `kindlast_migrator`, none has a delete trigger, and
`kindlast_app` holds no DELETE grant on any of them. The paragraphs below are
written about `audit_log` because that is where the shape was established, and
every word of them is true of the other two.

The absence of a delete trigger is deliberate on all three and it is what makes
the erasure procedure below possible: a `BEFORE DELETE` trigger would raise
inside the cascade from `organisations` and turn a single erasure statement into
an incident.

`db/README.md` says nothing deletes from `audit_log`, and that it is enforced
rather than observed. Both halves are true, and the enforcement is narrower
than the sentence sounds:

- `audit_log_no_update` is a **`BEFORE UPDATE`** trigger. There is no delete
  trigger.
- `kindlast_app` holds no `DELETE` grant on the table.

The UPDATE half is stronger than it looks. The trigger refuses **even
`kindlast_migrator`**, which is worth knowing because the migrator bypasses RLS
and holds every grant, so it is otherwise the role that can do anything:

```
ERROR:  audit_log is append-only: UPDATE on row ... is not permitted
```

The DELETE half has no equivalent, and there is a cascade pointed straight at
it:

```
audit_log.org_id  ->  organisations(id)  ON DELETE CASCADE
```

**Deleting an organisation deletes its audit log, silently, with no trigger in
the way.** Verified: one row before the delete, zero after, no error and no
warning. That is coherent, because erasing an organisation is exactly the
operation that should remove its records, and the product still cannot touch
the log. But "nothing deletes from `audit_log`" and "deleting an organisation
cascades the whole log away" are both true and read as a contradiction, so an
operator has to be told which is which before they run a delete.

The shape to hold on to: **the log cannot be rewritten by anyone, and can be
removed only wholesale, only by removing the organisation it belongs to.**
There is no way to delete one inconvenient row, which is the property that
matters.

Every tenant table cascades from `organisations` the same way. That is what
makes whole-organisation erasure a single statement.

## Answering an access request

For a person who signs in. Run as `kindlast_migrator`, which bypasses RLS, so
you are not restricted to one organisation's view.

**1. Resolve the person to a `user_id`.** The id is derived from the OIDC
issuer and subject, so start from the address they wrote to you with:

```sql
select user_id, email, display_name, issuer, subject, created_at
from user_identities
where lower(email) = lower('person@example.com');
```

**2. Find every organisation they belong to.** Their data is spread across all
of them, and an answer covering one is not an answer:

```sql
select m.org_id, o.slug, o.name, m.role
from memberships m join organisations o on o.id = m.org_id
where m.user_id = '<user_id>';
```

**3. Collect what they did and what is held about them.** `audit_log` is
usually the bulk of it, and it holds **two** references to people: `user_id`,
who acted, and `approving_user_id`, who authorised it. Both are `NOT NULL`, so
there is no such thing as an audit row that names only one person, and a query
filtering on `user_id` alone will miss every act somebody approved but did not
perform:

```sql
select * from audit_log
where user_id = '<user_id>' or approving_user_id = '<user_id>';

select * from onboarding_messages   where created_by = '<user_id>';
select * from notification_preferences where user_id = '<user_id>';
select * from findings              where approved_by = '<user_id>';
select * from processing_activities where created_by = '<user_id>';
select * from ai_systems            where created_by = '<user_id>';
select * from dsars                 where created_by = '<user_id>';
select * from dsar_trail_entries    where created_by = '<user_id>';
```

The last of those is worth a sentence. A trail entry is evidence about how a
response to somebody else's request was assembled, so `created_by` on it names
the handler and `detail` may describe the data subject. An access request from a
handler reaches the first; an access request from a data subject reaches the
second, and there is no id to search by, so search `detail` and `source` by value
the way step 4 does for addresses.

**4. Search by address as well as by id**, because of the not-a-user category
above. This is the step most likely to be skipped:

```sql
select * from invitations where lower(email) = lower('person@example.com');
select * from transactional_outbox
where lower(recipient_email) = lower('person@example.com');
select * from notification_preferences
where lower(email) = lower('person@example.com');
```

The middle one returns less than it used to, and that is deliberate rather than
a bug: a redacted outbox row has an empty `recipient_email`, so it cannot match
an address search and holds nothing to report. `invitations` still answers "was
this person invited, when, and did they accept", which is the part of the
question that has an answer worth giving. See the retention section below.

**5. Zitadel holds the rest.** Profile, credentials metadata and session
records live in the `zitadel` database on `postgres-platform`. Export through
Zitadel's own admin API rather than by reading its tables, because its schema
is its own and reading it directly will not survive an upgrade.

**6. Redis holds live sessions.** `web:session:*` keyed by an opaque session id,
so there is no way to select by user without reading values. For an access
request this is rarely worth including: it is ephemeral, and the durable
version of the same fact is in Zitadel.

## Answering an erasure request

Two shapes, and they are genuinely different.

### One person, whose organisations continue

Order matters, for the reason in the previous section.

**1. Establish what will be left behind before deleting anything.** Run step 3
of the access procedure and keep the output. It is the only record of what the
`created_by` values pointed at once they are orphaned.

**2. Decide about `audit_log` before touching it.** This is the open legal
question below, not an operator's call to make on the day.

**3. Remove the domain rows that are about them rather than authored by them:**

```sql
delete from notification_preferences where user_id = '<user_id>';
delete from capability_tokens         where user_id = '<user_id>';
delete from invitations where lower(email) = lower('person@example.com');
```

**4. Decide what happens to authorship.** `created_by` and `approved_by` record
which human did something and are never used for isolation. Deleting the
records because of who authored them would remove a customer's compliance data,
which is not what was asked. Anonymising in place is usually right, and it is a
deliberate decision either way.

**`dsar_trail_entries` cannot be anonymised in place.** Its UPDATE trigger
refuses the migrator, so the option available on every other table is not
available here: the row is evidence about a statutory response, and the whole
point of the trigger is that nobody can revise one after the fact. The choices
are to leave it, or to delete the DSAR it belongs to and take the trail with it,
which removes a customer's compliance evidence. That is the same Article 17(3)
question the open one below asks about `audit_log`, so escalate it the same way
rather than choosing on the day.

**5. Remove membership, then identity, in that order:**

```sql
delete from memberships     where user_id = '<user_id>';
delete from user_identities where user_id = '<user_id>';
```

**6. Delete the person in Zitadel**, through its admin API. Until this happens
they can still sign in and be provisioned again on the spot, which would undo
the work above.

**7. Drop their sessions from Redis.** They survive restarts, so leaving them
leaves a working session for a deleted user:

```bash
docker compose -f deploy/compose.yaml exec redis redis-cli --scan --pattern 'web:session:*'
```

Read each value to find the ones carrying that `user_id`, and delete those.
Flushing everything works too and signs out every other user, so prefer it only
on a stack nobody is using.

### A whole organisation

One statement, because every tenant table cascades:

```sql
delete from organisations where id = '<org_id>';
```

Then the members, if they belong nowhere else, via the per-person procedure.

**Know what this takes with it.** The organisation's `audit_log` goes, along
with its findings, records, DSARs, onboarding conversation, profile and
outbox. That is the intended effect of erasing an organisation and it is also
irreversible, so confirm the organisation id against `slug` and `name` before
running it, not after.

## What remains afterwards, by design

**Backups.** They are outside every procedure above, and there is currently no
backup runbook at all (ENT-239). Until there is, an erasure completed against
the live database is not an erasure completed against every copy, and saying so
is more honest than implying otherwise.

**The audit log, subject to the question below.**

## Evidence retention, decided rather than deferred (ENT-231)

Integrations fetch from a customer's own systems, so `org_evidence` now holds
content that came from a helpdesk, a document store or a cloud account. That
raises a retention question, and ENT-231 asks for it to be decided before the
first connector ships rather than after.

**The decision is: for the life of the organisation, and no timer.**

The argument is that an observation is what a finding was derived from. A
compliance record whose evidence expired on a schedule is one where last year's
finding can no longer be checked, and "you can check the claim against the
source" is the thing this product sells. A ninety-day expiry would quietly turn
every finding older than ninety days into an assertion.

What bounds the exposure instead is three things that are already true:

- **Redaction runs before storage**, in the gateway, so the unredacted form
  never reaches the process that writes the row. Bearer tokens, API keys,
  private key blocks, email addresses, IBANs and card-shaped digit runs are
  replaced with a marker. Names and prose are deliberately not, because a
  redactor keen enough to remove those would remove the sentences a finding is
  derived from.
- **Erasure is complete and is one gesture.** `org_evidence`,
  `integration_fetches`, `integration_consents`, `integration_tools` and
  `integrations` all cascade from `organisations`, and none of them grants
  `delete` to anybody, so there is no per-row delete button that could make
  "correct this" and "erase this" the same click. The erasure procedure above
  therefore covers evidence with no extra step.
- **Revocation stops collection without destroying the record.** Revoking a
  connection is terminal and stops every future fetch; what was already read
  stays, because deleting it would rewrite the compliance record rather than
  narrow it.

**What is NOT decided here** is a per-class expiry that a customer could set
themselves, which is the fuller answer §25 wants and which needs a
customer-visible setting rather than a constant in a Go package. Recorded as
open, in the same spirit as the question below.

## Outbox retention, and why it is redaction (ENT-242)

`transactional_outbox` is the one table in the schema that holds a bearer
credential in the clear. Its body is the rendered invitation, the rendered
invitation contains the accept link, and the accept link contains the raw token
in a path segment, because the link is the message. `invitations` two tables
away stores only that token's hash and 00003 says why: a database dump must not
yield a working invitation.

That exception was always meant to be bounded by the dispatcher draining the
row. Nothing removed the row afterwards, so it was not bounded at all, and every
address and message body ever sent was still there. Migration 00030 bounds it.

**The decision is: redact, and delete nothing.**

The row is two separable things. A delivery fact, and a rendered message holding
a person's address and a live credential. Only the second is personal data.
Deleting the row would drop the data by throwing away the fact; keeping the row
holds the fact by keeping the data. So the reclaim clears `recipient_email`,
`subject`, `body_text`, `body_html` and `last_error`, stamps `redacted_at`, and
leaves the rest alone. The only thing that removes a row from this table is
still the cascade from `organisations`, so the erasure procedures above need no
extra step.

What that means for each state, which is what an operator actually needs:

| State | What happens | When |
|---|---|---|
| Delivered | Body, subject, address and error cleared; `status`, `sent_at` and `attempts` kept | The earlier of seven days after delivery and the invitation ceasing to be acceptable |
| Undelivered, invitation dead | Moved to `failed`, `last_error` says it was abandoned, then redacted as above | On the next pass after the invitation expires or is accepted |
| Undelivered, invitation live | **Nothing, ever** | Not reachable at any retention window, including zero |

The last row is the one to understand before touching any of this. The raw token
exists nowhere else in the system, so clearing that body destroys an invitation
somebody is waiting for; reissue is the only cure and nobody could tell which
ones needed it. The rule protecting it is in the database and takes no argument
from the caller, precisely so that editing the period cannot reach it.

**Why seven days.** It is `postgres.InvitationLifetime`, so a delivered body is
kept exactly as long as the token in it can still be used and not one day
longer. Shorter loses the answer to "what did we actually send this person",
which is what somebody asks when a link did not work, and that arrives within
days. Longer keeps a dead credential and an address for nothing. It is an upper
bound rather than the usual case: an invitation accepted ten minutes after it
lands has its body cleared on the next hourly pass.

**Who runs it.** The outbox dispatcher inside `core-api`, on its own hourly
ticker beside the ten-second delivery ticker, through
`reclaim_transactional_outbox`. It is safe in more than one replica and is not a
singleton: both statements take their rows `for update skip locked` and every
predicate tests `redacted_at is null`, so replicas take disjoint sets and the
work is idempotent. It moves to Temporal at build-order step 8 with the drain it
sits beside.

**The sibling tables were checked and left alone.** `notification_outbox`,
`deadline_alert_log` and `weekly_briefing_log` do not have this problem. None
carries a body, a subject or an address: their personal data is a `user_id`,
which resolves to a person only through `user_identities`, and all three cascade
from `organisations`. `notification_outbox` resolves its recipient at dispatch
time by design, so there is no rendered message in it to redact. Recorded here
so the check does not have to be repeated.

The longer argument, including why this is the opposite conclusion to
`audit_log`'s, is in [`db/README.md`](../db/README.md).

## The open question, stated rather than answered

`audit_log.before` and `audit_log.after` hold whatever the acted-on row
contained. For a DSAR that can include a data subject's name, and for a trail
entry it can include whatever a handler wrote in `detail`. The same question
therefore reaches `dsar_trail_entries` itself, which is append-only for exactly
the reason that makes the question hard: the record is worth having because
nobody can revise it.

An erasure request reaching those payloads needs a decision about whether an
accountability record is exempt under **Article 17(3)**. That is a legal call,
not an engineering one. It is recorded in `db/README.md` and tracked in the
design doc's §25.

**Do not resolve it in a migration, and do not resolve it at the console on the
day a request arrives.** If you are here because a request has arrived and this
question is still open, escalate rather than choose.

## Verification

Every schema fact above was read out of the running database rather than out of
the migrations: grants per role from `information_schema.role_table_grants`,
the absent foreign keys from `constraint_column_usage`, the trigger from
`pg_trigger`, the cascades from `referential_constraints`.

Both procedures were then walked against the compose stack on 2026-08-17,
against a synthetic organisation created for the purpose and removed by the
procedure itself, leaving zero rows behind in all seven tables checked. What
that walk established, as opposed to asserted:

- All four access steps return what they claim to, including step 4, which
  found the address in `invitations`, `transactional_outbox` and
  `notification_preferences` where the `user_id` search found nothing.
- `UPDATE` on `audit_log` is refused **as `kindlast_migrator`**, with the
  trigger's own message.
- Deleting `user_identities` succeeded with **no error**, leaving an
  `audit_log` row whose `user_id` resolves to nothing.
- Deleting the organisation took the audit row with it: one before, zero after.

The walk also corrected the draft twice, which is the argument for walking it:
`audit_log.approving_user_id` turned out to be `NOT NULL`, and the UPDATE
trigger turned out to bind the migrator too, not only the application role.

Re-verify after any migration that adds a tenant table, and extend the role
table above at the same time.

**ENT-226 (migration 00024) added `dsar_trail_entries`**, and the table above was
extended with it. What was verified against the running schema for that table:
its grants (`select` and `insert` to `kindlast_app`, nothing else, nothing to any
other role), that `UPDATE` on it is refused as `kindlast_migrator` with the
trigger's own message, and that deleting the request or the organisation takes
its entries with it. The two full procedures above were **not** re-walked
end to end for it; the queries added to them are the same shape as the ones
beside them and have not been run against a synthetic organisation the way the
2026-08-17 walk was.
