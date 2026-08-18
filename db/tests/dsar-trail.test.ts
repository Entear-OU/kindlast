/**
 * The DSAR trail: the boundary around evidence a customer can read (ENT-226,
 * 00024).
 *
 * `dsars.responded_at` is an assertion. This table is what stands behind it: the
 * stores that were searched for a data subject's personal data, when each was
 * searched, what came back, and what went into the response. A regulator reading
 * the register reads this, so two properties matter more than anything else the
 * table does.
 *
 * ONE: IT IS SOMEBODY'S. A trail entry belongs to an organisation and to one of
 * that organisation's requests, and neither the application role nor a bug in
 * the middleware can attach an entry to another tenant's DSAR. That is asserted
 * twice on purpose: the RLS with-check refuses the row, and the composite
 * foreign key onto `dsars (id, org_id)` refuses the pair even where RLS is not
 * the thing doing the refusing.
 *
 * TWO: IT CANNOT BE QUIETLY REVISED. Evidence a producer can edit after the fact
 * is worth less than evidence it cannot, which is the argument `audit_log` and
 * `agent_runs` already rest on. So UPDATE is refused by a trigger that binds
 * even `kindlast_migrator`, and `kindlast_app` holds no DELETE grant.
 *
 * There is deliberately NO delete trigger, and that is the same shape
 * `docs/personal-data-runbook.md` describes for `audit_log`: the trail cannot be
 * rewritten by anyone, and can be removed only wholesale, by removing the
 * organisation or the request it belongs to. A BEFORE DELETE trigger would make
 * `delete from organisations where id = ...` fail, which is the single statement
 * the erasure procedure depends on.
 *
 * PROVEN ABLE TO FAIL. Three deliberate breakages, run against the compose
 * stack and all reverted:
 *
 *   - Replacing `dsar_trail_entries_insert_org`'s with-check with `true` turns
 *     "cannot be written into another organisation" red on its own, and nothing
 *     else. That is the shape that says these test the boundary rather than the
 *     plumbing.
 *   - Narrowing `dsar_trail_entries_dsar_fkey` to `(dsar_id)` alone turns
 *     "cannot be attached to another organisation's request" red, and "is
 *     scoped to the caller's organisation" with it, because the entry that
 *     should have been refused is now in Alpha's count.
 *   - Dropping the `dsar_trail_entries_no_update` trigger turns "cannot be
 *     updated by the migrator either" red and leaves the isolation tests green.
 *     The application half stays green through it, which is the point: the
 *     grant and the trigger refuse different callers.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  setTenant,
  isStackReachable,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID() // owner of Alpha
const bob = randomUUID() // owner of Beta, a member of nothing else

const dsarA = randomUUID() // Alpha's request
const dsarB = randomUUID() // Beta's request

let migrator: Client
let app: Client

async function seedOrg(org: string, label: string, owner: string) {
  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `${label} ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, owner],
  )
}

async function seedDsar(id: string, org: string, owner: string) {
  await migrator.query(
    `insert into dsars (id, org_id, created_by, subject_name, request_type,
                        status, received_at, response_due_at)
     values ($1, $2, $3, 'A Data Subject', 'access', 'open', now(),
             now() + interval '30 days')`,
    [id, org, owner],
  )
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  await seedOrg(orgA, 'Trail Alpha', ada)
  await seedOrg(orgB, 'Trail Beta', bob)
  await seedDsar(dsarA, orgA, ada)
  await seedDsar(dsarB, orgB, bob)
})

afterAll(async () => {
  if (!reachable) return
  // Trail entries cascade from the organisation, which is the whole point of
  // the erasure shape this suite asserts. Deleting the organisations is
  // therefore enough, and doing it that way exercises the cascade one more
  // time.
  await migrator.query(`delete from organisations where id = any($1)`, [
    [orgA, orgB],
  ])
  await migrator.end()
  await app.end()
})

describe.skipIf(!reachable)('writing a trail entry', () => {
  it('is allowed for the caller’s own request', async () => {
    await setTenant(app, orgA, ada)

    await app.query(
      `insert into dsar_trail_entries
         (org_id, dsar_id, source, action, detail, occurred_at, created_by)
       values ($1, $2, 'HR system (Personio)', 'found',
               'Employment record for the subject', now(), $3)`,
      [orgA, dsarA, ada],
    )

    const { rows } = await migrator.query(
      `select count(*)::int as n from dsar_trail_entries where dsar_id = $1`,
      [dsarA],
    )
    expect(rows[0].n).toBe(1)
  })

  it('cannot be attached to another organisation’s request', async () => {
    // The bug this refuses is a handler that took `dsar_id` from the request
    // body and never checked whose it was. Ada is signed into Alpha and names
    // Beta's request; the row is refused rather than written.
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into dsar_trail_entries
           (org_id, dsar_id, source, action, occurred_at, created_by)
         values ($1, $2, 'CRM', 'searched', now(), $3)`,
        [orgA, dsarB, ada],
      ),
    ).rejects.toThrow(/foreign key|violates/i)
  })

  it('cannot be written into another organisation', async () => {
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into dsar_trail_entries
           (org_id, dsar_id, source, action, occurred_at, created_by)
         values ($1, $2, 'CRM', 'searched', now(), $3)`,
        [orgB, dsarB, ada],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('must name a store', async () => {
    // A trail entry whose source is blank records that somebody looked
    // somewhere, which is not a fact anyone can check. The constraint is in the
    // database because it must hold whoever writes.
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into dsar_trail_entries
           (org_id, dsar_id, source, action, occurred_at, created_by)
         values ($1, $2, '   ', 'searched', now(), $3)`,
        [orgA, dsarA, ada],
      ),
    ).rejects.toThrow(/dsar_trail_entries_source_not_blank/i)
  })

  it('must say who or what produced it', async () => {
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into dsar_trail_entries
           (org_id, dsar_id, source, action, occurred_at)
         values ($1, $2, 'CRM', 'searched', now())`,
        [orgA, dsarA],
      ),
    ).rejects.toThrow(/dsar_trail_entries_attributed/i)
  })

  it('refuses an action outside the recorded vocabulary', async () => {
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into dsar_trail_entries
           (org_id, dsar_id, source, action, occurred_at, created_by)
         values ($1, $2, 'CRM', 'probably-fine', now(), $3)`,
        [orgA, dsarA, ada],
      ),
    ).rejects.toThrow(/dsar_trail_entries_action_check/i)
  })
})

describe.skipIf(!reachable)('reading a trail', () => {
  it('is scoped to the caller’s organisation', async () => {
    await migrator.query(
      `insert into dsar_trail_entries
         (org_id, dsar_id, source, action, occurred_at, created_by)
       values ($1, $2, 'Beta CRM', 'searched', now(), $3)`,
      [orgB, dsarB, bob],
    )

    await setTenant(app, orgA, ada)
    const { rows } = await app.query(
      `select count(*)::int as n from dsar_trail_entries`,
    )
    // Alpha's one entry, and none of Beta's. A count rather than a lookup by
    // id, because "not visible" has to mean absent from the table's whole
    // world and not merely absent from one query.
    expect(rows[0].n).toBe(1)
  })

  it('is invisible to a non-member even with the right organisation set', async () => {
    // The membership `exists` half of the two-GUC form. A middleware bug that
    // set Alpha's id for Bob still reads zero rows.
    await setTenant(app, orgA, bob)
    const { rows } = await app.query(
      `select count(*)::int as n from dsar_trail_entries`,
    )
    expect(rows[0].n).toBe(0)
  })
})

describe.skipIf(!reachable)('a written trail entry is evidence', () => {
  it('cannot be updated by the application', async () => {
    await setTenant(app, orgA, ada)

    // No update grant at all, so this is refused before any policy is
    // consulted.
    await expect(
      app.query(`update dsar_trail_entries set detail = 'nothing to see'`),
    ).rejects.toThrow(/permission denied/i)
  })

  it('cannot be updated by the migrator either', async () => {
    // The half that matters. The migrator owns the schema and bypasses RLS, so
    // grants and policies do not constrain it: the claim "this trail is what
    // happened" is only worth something if nobody, including us, can revise a
    // row after the fact.
    await expect(
      migrator.query(`update dsar_trail_entries set detail = 'nothing to see'`),
    ).rejects.toThrow(/append-only/i)
  })

  it('cannot be deleted by the application', async () => {
    await setTenant(app, orgA, ada)

    await expect(app.query(`delete from dsar_trail_entries`)).rejects.toThrow(
      /permission denied/i,
    )
  })

  it('goes when the request it belongs to goes, and only then', async () => {
    // Erasure is wholesale or not at all, which is the same shape
    // docs/personal-data-runbook.md records for audit_log. There is no delete
    // trigger, deliberately, because one would break the single statement the
    // erasure procedure runs.
    const doomed = randomUUID()
    await seedDsar(doomed, orgA, ada)
    await migrator.query(
      `insert into dsar_trail_entries
         (org_id, dsar_id, source, action, occurred_at, created_by)
       values ($1, $2, 'Doomed store', 'searched', now(), $3)`,
      [orgA, doomed, ada],
    )

    await migrator.query(`delete from dsars where id = $1`, [doomed])

    const { rows } = await migrator.query(
      `select count(*)::int as n from dsar_trail_entries where dsar_id = $1`,
      [doomed],
    )
    expect(rows[0].n).toBe(0)
  })
})

describe.skipIf(!reachable)('the shape the isolation suite depends on', () => {
  it('carries org_id, with RLS enabled and forced', async () => {
    // force-rls.test.ts sweeps every table with an org_id column, so this is
    // belt and braces rather than the only guard. It is here because a reader
    // of this file should not have to know that the sweep exists.
    const { rows } = await migrator.query(`
      select c.relrowsecurity as enabled, c.relforcerowsecurity as forced
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public' and c.relname = 'dsar_trail_entries'
    `)
    expect(rows[0]).toEqual({ enabled: true, forced: true })
  })
})
