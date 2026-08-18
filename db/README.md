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

One open question, deliberately not answered here: `before` and `after` hold
whatever the acted-on row contained, which for a DSAR includes a data subject's
name. An erasure request reaching those payloads needs a decision about whether
an accountability record is exempt under Article 17(3), which is a legal call
rather than an engineering one. Raise it; do not solve it in a migration.
