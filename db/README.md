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
manual path in the product. The table carries an append-only trigger and
`kindlast_app` holds no delete grant on it, so the absence is enforced rather
than merely observed.

This is written down because ENT-223 asked for it to be a decision rather than
an omission, and because from the outside the two look identical.

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

## `transactional_outbox` is redacted, and that is the same decision reversed

A reader who has just read why `audit_log` keeps everything forever will
reasonably ask why the table two sections down is cleared out on a timer. The
answer is that they hold different things, and the difference is worth stating
rather than leaving to be inferred.

`transactional_outbox` rows carry `recipient_email` and the full rendered
message. For the only kind that exists today, an invitation, that body contains
the accept link, and the accept link contains the raw token in a path segment,
because the link **is** the message. Two tables away, `invitations` stores only
that token's hash, and 00003 says why: an invitation token is a bearer
credential and a database dump must not yield a working one. The outbox is the
single place that rule is suspended, and it was meant to be suspended only for
as long as it takes the dispatcher to deliver the row. Nothing removed it
afterwards, so the suspension had no end (ENT-242, migration 00030).

**The row is two separable things.** It is a delivery fact, and it is a rendered
message holding a person's address and a live credential. Only the second is
personal data. Deleting the row drops the data by throwing away the fact.
Keeping the row holds the fact by keeping the data. Redaction is the only option
that does not force that trade, so **00030 deletes nothing**: it clears
`recipient_email`, `subject`, `body_text`, `body_html` and `last_error`, stamps
`redacted_at`, and leaves the rest of the row where it is. The only thing in the
deployment that removes a row from this table remains the cascade from
`organisations`, which is how erasing an organisation already works.

**Why this is not the `audit_log` argument.** The audit log is the evidence, and
its content is the thing a regulator would be shown. The outbox is the envelope.
What a customer or a regulator would be shown about an invitation is in
`invitations`: the address, the role offered, who invited them, when, and
whether they accepted, none of it touched by any of this. What the outbox adds
on top is the rendered text of the message and the record of getting it out of
the door. The second is worth keeping; the first, a few days later, is a spent
credential and somebody's email address.

Put beside [`docs/backup-and-restore.md`](../docs/backup-and-restore.md), which
is the same question from the loss side, the three make one position rather than
three: the record must not be deleted, it must not be lost, and it must not
carry personal data it no longer needs to prove what it proves.

**When redaction happens.** At the earlier of two moments. Seven days after
delivery, which is `postgres.InvitationLifetime`, so the body is kept exactly as
long as the token inside it can still be used and not one day longer. Or as soon
as the invitation it carries stops being acceptable, whether it expired or was
accepted, because a spent link is worth nothing to anybody and holding an
address for the rest of a window to keep it would be the wrong trade. The
seven-day figure is a decision and lives in Go as
`dispatch.DeliveredBodyRetention`; the rule about which rows may be touched at
all is an invariant and lives in the database.

**What happens to a row that never delivers.** This is the case a
delivery-triggered rule leaves behind forever, so it has its own answer. An
undelivered message whose invitation can no longer be accepted is moved to
`failed`, given a `last_error` saying it was abandoned, and redacted. That is
also the first thing in this codebase to write `failed`, which 00014 reserved
for giving up: the drain claims `status = 'pending'` and has no maximum attempt
count, so before this an undeliverable message was retried every ten seconds for
as long as the deployment lived.

**What is never touched.** A message that has not been delivered and whose
invitation can still be accepted, at any window including zero. The raw token
exists nowhere else, so clearing that body destroys an invitation somebody is
waiting for, reissue is the only cure, and nobody could tell which ones needed
it. The predicate protecting it takes no argument from the caller, which is why
the window and the rule live in different places.

**Why a `SECURITY DEFINER` function does the work.** Deciding an undelivered
message's fate means asking whether its invitation can still be accepted, which
is a read of `invitations`. `kindlast_agent` has no grant there by design
(00008), because a role that can fabricate a finding should not also be able to
enumerate every invited address in the deployment, and RLS cannot close the gap:
a policy subquery is evaluated with the querying role's privileges and would
need the same grant. The function answers that one question about rows it is
already looking at. It is the argument 00015 made for `notification_recipients`,
reaching the same conclusion.

**The sibling tables, checked and left alone.** `notification_outbox`,
`deadline_alert_log` and `weekly_briefing_log` were examined for the same
problem and do not have it. None carries a body, a subject or an address: their
personal data is a `user_id`, an internal identifier that resolves to a person
only through `user_identities`, and every row of all three cascades from
`organisations`. `notification_outbox` in particular holds a `finding_id` and a
status and resolves its recipient at dispatch time, deliberately, so there is no
rendered message in it to redact. They are noted here so that "we looked and
these are different" is recorded rather than having to be worked out again.
