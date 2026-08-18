# Backup and restore

The compliance record is the artefact a customer would hand a regulator. This
page says how to copy it and, more importantly, how to put it back.

`db/README.md` spends three careful paragraphs establishing that nothing may
ever delete from `audit_log`: no retention job, an append-only trigger, and no
delete grant for `kindlast_app`. **A dead volume produces exactly the outcome
that reasoning refuses.** This page is the other half of "the record survives".

## What is irreplaceable, and what is not

| Volume | Holds | If you lose it |
|---|---|---|
| `postgres-app-data` | The domain schema: organisations, findings, records, `audit_log`, memory, delegations | **Irreplaceable.** This is the compliance record |
| `postgres-platform-data` | Zitadel: users, credentials, the project, the role grants | **Effectively irreplaceable.** See the identity note below |
| `zitadel-machinekey` | The seed's generated client credentials and the audience file | Reproducible by re-running the seed, but only against a surviving Zitadel |
| `redis-data` | Sessions and the revocation deny-list | **Disposable.** Everyone signs in again |
| `deploy/models/` (host path) | The GGUF weights | **Reproducible.** `model-init` refetches and verifies the SHA256 |

Two things are deliberately not backed up, and the omissions are decisions
rather than oversights:

- **The regulatory corpus.** It is reproducible from `data/corpus/` and the
  pack loader, and `TestReIngestingTheWholeCorpusChangesNothing` asserts that
  re-ingesting it changes nothing. Restoring it from a dump and re-ingesting it
  are the same result, so it does not need its own path.
- **The model file.** It is pinned by digest and refetched on demand. Backing up
  gigabytes that a checksum can reproduce buys nothing.

### Why losing Zitadel is worse than it looks

Every `user_id` in the domain schema is derived from the issuer and the subject
claim. `docs/core-api-configuration.md` calls changing the issuer an identity
migration rather than a settings edit, and the same applies here: restore
`postgres-app` next to a *rebuilt* Zitadel and the domain rows reference
identities that no longer exist. The organisations survive and nobody can sign
in to them.

So the two databases are one backup unit, not two.

## The ordering and skew problem

`postgres-app` and `postgres-platform` are separate databases dumped
separately, so two dumps taken minutes apart can disagree. The failure is
specific: a user who signed up between the two dumps exists in one and not the
other.

**Take both dumps as close together as possible, and restore both or neither.**
Which order you restore in does not matter, because nothing is enforced across
the two databases by a constraint; what matters is that they come from the same
moment.

If they do skew, the direction to prefer is **platform newer than app**. An
identity with no domain rows is harmless (the user signs in and is provisioned).
A domain row with no identity is the broken case.

`pg_dump` does not solve this, and no logical dump tool does. Point-in-time
recovery does, by letting you restore both to the same timestamp, which is the
main argument for adding it later. See "When to reach for more" below.

## Taking a backup

Both commands run against the compose stack. Neither interrupts service:
`pg_dump` takes a consistent snapshot without blocking writers.

```bash
# The compliance record.
docker exec kindlast-postgres-app \
  pg_dump -U kindlast_migrator -d kindlast -Fc -f /tmp/kindlast-app.dump
docker cp kindlast-postgres-app:/tmp/kindlast-app.dump ./kindlast-app.dump

# Identity, taken at the same time.
docker exec kindlast-postgres-platform \
  pg_dump -U postgres -d zitadel -Fc -f /tmp/zitadel.dump
docker cp kindlast-postgres-platform:/tmp/zitadel.dump ./zitadel.dump
```

`-Fc` is the custom format: compressed, and restorable selectively with
`pg_restore`. Measured on a development stack, the two dumps were 456 KB and
328 KB.

Store them somewhere that is not the machine running the stack. A backup on the
same disk as the database protects against exactly one failure mode, and not
the common one.

## Restoring

**Restore into a fresh database and switch to it, rather than over a live one.**
`pg_restore --clean` exists and is the wrong reflex here: it drops objects as it
goes, so a restore that fails halfway leaves you with neither the old database
nor the new one.

```bash
docker exec kindlast-postgres-app \
  psql -U postgres -c 'create database kindlast_restored owner kindlast_migrator'

docker exec kindlast-postgres-app \
  pg_restore -U postgres -d kindlast_restored --no-owner --role=kindlast_migrator \
  /tmp/kindlast-app.dump
```

Then verify (below), then point the stack at it by renaming, with everything
that connects stopped:

```bash
docker compose -f deploy/compose.yaml stop core-api intelligence workers web
docker exec kindlast-postgres-app psql -U postgres \
  -c 'alter database kindlast rename to kindlast_old' \
  -c 'alter database kindlast_restored rename to kindlast'
docker compose -f deploy/compose.yaml start core-api intelligence workers web
```

Keep `kindlast_old` until you are satisfied. It costs disk and it is the only
thing standing between you and a bad restore.

### Two errors that are expected

A restore of this schema prints two errors and one warning, and they are
benign:

```
pg_restore: error: could not execute query: ERROR:  must be owner of extension pgcrypto
pg_restore: error: could not execute query: ERROR:  must be owner of extension vector
pg_restore: warning: errors ignored on restore: 2
```

Both are `COMMENT ON EXTENSION` statements. The extensions themselves restore;
only their comments do not, because commenting on an extension requires owning
it. **Nothing is missing.** This is written down because a runbook that does not
mention them invites an operator to conclude the restore failed and start over.

## Verifying a restore

A backup nobody has restored is a hope, and a restore nobody has verified is a
different hope. Check the security properties, not only the row counts: this
schema's value is that tenant isolation holds, and a restore that silently drops
RLS would look fine in a row count and be a data breach.

```sql
-- Every table has RLS enabled AND forced. Both numbers must equal the table count.
select count(*) filter (where relrowsecurity)      as rls_enabled,
       count(*) filter (where relforcerowsecurity) as rls_forced,
       count(*)                                    as tables
  from pg_class c join pg_namespace n on n.oid = c.relnamespace
 where n.nspname = 'public' and c.relkind = 'r';

-- Policies, the append-only trigger, and the application's grants.
select (select count(*) from pg_policies where schemaname = 'public')            as policies,
       (select count(*) from pg_trigger t join pg_class c on c.oid = t.tgrelid
         where c.relname = 'audit_log' and not t.tgisinternal)                   as audit_triggers,
       (select count(*) from information_schema.role_table_grants
         where table_schema = 'public' and grantee = 'kindlast_app')             as app_grants;
```

Run the same two queries against the source and compare. Then run the isolation
suite against the restored database, which is the real check:

```bash
bun run test:db
```

### What was actually measured

This procedure was walked on 2026-08-18 against the compose stack, restoring
into a scratch database rather than over the live one. Source and restored
agreed exactly:

| Property | Source | Restored |
|---|---|---|
| Tables with RLS enabled | 43 | 43 |
| Tables with RLS **forced** | 43 | 43 |
| Policies | 136 | 136 |
| Append-only triggers on `audit_log` | 1 | 1 |
| Grants to `kindlast_app` | 85 | 85 |
| `organisations` / `findings` / `audit_log` rows | 14 / 1 / 2 | 14 / 1 / 2 |

**The security boundary survives a dump and restore**, which is the property
worth knowing and the reason to check it this way rather than by counting rows.

One row count did differ, and the reason is worth recording: `obligations` read
17 in the dump and 16 in the source when compared minutes later, because another
process ingested and removed a fixture in between. **A dump is a point in time,
and a live database is not.** Compare a restore against the dump's moment, not
against a source that has moved on.

## When to reach for more

`pg_dump` gives you a recovery point of "the last time you ran it". If losing a
day is acceptable, a nightly dump is a complete answer and its restore is a
command an operator can read.

If it is not, the upgrade is WAL archiving with **pgBackRest**, **WAL-G** or
**Barman**, which give incremental backup and point-in-time recovery. That is a
genuine capability difference: minutes lost instead of a day, and both databases
restorable to the same timestamp, which is the skew problem above solved rather
than mitigated.

It is deliberately not shipped here. A self-hoster who cannot restore is not
helped by a more capable tool they never configured, and adding WAL archiving
later does not invalidate anything on this page.

## What is not automated

Nothing on this page runs on a schedule. There is no backup service in
`deploy/compose.yaml`, and that is a decision for now rather than an omission:
scheduling is trivial (`cron` calling the two commands above) and the hard part,
which is knowing that a restore works, is what this page delivers. Shipping a
backup container is its own piece of work and it should not be the thing that
delays writing the procedure down.
