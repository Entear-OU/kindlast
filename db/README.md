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
- The handful of `SECURITY DEFINER` functions that exist because RLS
  structurally cannot express the check: `app_org_role`,
  `app_org_member_count`, `accept_invitation`, and
  `notification_recipients`

Each definer function carries its justification in the migration that creates
it. A definer function is how RLS gets bypassed by accident, so adding a fifth
means writing down why the fourth's reasoning does not cover you.

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
