/**
 * The isolation test (ENT-192), written before the policies were ported.
 *
 * This is the test §14.1 of the core-api-surface doc demands: on a plain
 * Postgres container the application role is one misconfiguration away from
 * being a superuser or a table owner, and either bypasses RLS silently. Every
 * policy keeps passing, tenant isolation is simply gone. So the suite proves
 * four things, not one:
 *
 *   1. A member of org 1 with the GUCs set reads org 1's rows and reads zero
 *      rows from org 2 (two-org isolation).
 *   2. A user with no membership anywhere reads nothing at all, even with the
 *      GUCs pointing at a real org (the `exists` clause in every policy, which
 *      is what survives a middleware bug that sets the wrong org).
 *   3. The app role is genuinely unprivileged: is_superuser is off, and the
 *      role has neither BYPASSRLS nor table ownership.
 *   4. The superuser DOES read across orgs, proving the suite can tell
 *      enforcement apart from an empty database (the non-vacuity check).
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  setTenant,
  isStackReachable,
  SUPER_URL,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

// Two orgs, one member each, plus a user who belongs nowhere.
const orgA = randomUUID()
const orgB = randomUUID()
const userA = randomUUID()
const userB = randomUUID()
const strayUser = randomUUID()

// Per-org fixture chain: session -> profile -> watcher finding -> finding.
const ids = {
  a: {
    session: randomUUID(),
    profile: randomUUID(),
    watcher: randomUUID(),
    finding: randomUUID(),
    dsar: randomUUID(),
  },
  b: {
    session: randomUUID(),
    profile: randomUUID(),
    watcher: randomUUID(),
    finding: randomUUID(),
    dsar: randomUUID(),
  },
}

let migrator: Client
let app: Client
let superuser: Client

async function seedOrg(
  c: Client,
  org: string,
  user: string,
  f: (typeof ids)['a'],
): Promise<void> {
  // The slug is derived through org_slug() rather than written literally, for
  // the reason the seed fixture does the same: one rule, in one place. It is
  // NOT NULL as of ENT-198, so a fixture that omits it does not merely lack a
  // slug, it fails to insert at all.
  await c.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `test-org-${org.slice(0, 8)}`],
  )
  await c.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, user],
  )
  await c.query(
    `insert into onboarding_sessions (id, org_id, created_by, status)
     values ($1, $2, $3, 'completed')`,
    [f.session, org, user],
  )
  await c.query(
    `insert into compliance_profiles
       (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
     values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')`,
    [f.profile, org, user, f.session],
  )
  await c.query(
    `insert into watcher_findings
       (id, org_id, profile_id, kind, title, dedup_key)
     values ($1, $2, $3, 'profile_gap', 'isolation fixture', $4)`,
    [f.watcher, org, f.profile, `isolation-${f.watcher}`],
  )
  await c.query(
    `insert into findings
       (id, org_id, profile_id, watcher_finding_id, obligation_id, detected, proposed_action)
     select $1, $2, $3, $4, o.id, 'isolation fixture', 'none'
     from obligations o limit 1`,
    [f.finding, org, f.profile, f.watcher],
  )
  await c.query(
    `insert into dsars (id, org_id, created_by, subject_name, response_due_at)
     values ($1, $2, $3, 'Isolation Fixture', now() + interval '30 days')`,
    [f.dsar, org, user],
  )
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  superuser = await connect(SUPER_URL)
  await seedOrg(migrator, orgA, userA, ids.a)
  await seedOrg(migrator, orgB, userB, ids.b)
})

afterAll(async () => {
  if (!reachable) return
  // organisations cascades through memberships and every tenant table.
  await migrator.query(`delete from organisations where id in ($1, $2)`, [orgA, orgB])
  await Promise.all([migrator.end(), app.end(), superuser.end()])
})

describe.skipIf(!reachable)('two-org isolation as kindlast_app', () => {
  it('a member of org 1 reads their own org rows', async () => {
    await setTenant(app, orgA, userA)
    const tables = [
      'onboarding_sessions',
      'compliance_profiles',
      'watcher_findings',
      'findings',
      'dsars',
    ] as const
    for (const table of tables) {
      const r = await app.query(
        `select count(*)::int as n from ${table} where org_id = $1`,
        [orgA],
      )
      expect(r.rows[0].n, `${table} should show org A its own row`).toBe(1)
    }
  })

  it('a member of org 1 reads ZERO rows belonging to org 2', async () => {
    await setTenant(app, orgA, userA)
    const byId: Array<[string, string]> = [
      ['onboarding_sessions', ids.b.session],
      ['compliance_profiles', ids.b.profile],
      ['watcher_findings', ids.b.watcher],
      ['findings', ids.b.finding],
      ['dsars', ids.b.dsar],
    ]
    for (const [table, id] of byId) {
      const r = await app.query(`select count(*)::int as n from ${table} where id = $1`, [id])
      expect(r.rows[0].n, `${table} must hide org B's row from org A`).toBe(0)
    }
    // And the blunter form: filtering by the other org's id returns nothing.
    const cross = await app.query(
      `select count(*)::int as n from findings where org_id = $1`,
      [orgB],
    )
    expect(cross.rows[0].n).toBe(0)
  })

  it('a user with no membership reads nothing at all, even with the GUCs set to a real org', async () => {
    await setTenant(app, orgA, strayUser)
    for (const table of ['findings', 'dsars', 'compliance_profiles', 'organisations']) {
      const r = await app.query(`select count(*)::int as n from ${table}`)
      expect(r.rows[0].n, `${table} must be empty for a user with no membership`).toBe(0)
    }
  })

  it('a member cannot write a row into another org', async () => {
    await setTenant(app, orgA, userA)
    await expect(
      app.query(
        `insert into dsars (org_id, created_by, subject_name, response_due_at)
         values ($1, $2, 'Cross-org write', now() + interval '30 days')`,
        [orgB, userA],
      ),
    ).rejects.toThrow(/row-level security/)
  })
})

describe.skipIf(!reachable)('the app role is genuinely unprivileged', () => {
  it("current_setting('is_superuser') is off", async () => {
    const r = await app.query(`select current_setting('is_superuser') as s`)
    expect(r.rows[0].s).toBe('off')
  })

  it('kindlast_app has neither BYPASSRLS nor SUPERUSER in pg_roles', async () => {
    const r = await superuser.query(
      `select rolsuper, rolbypassrls from pg_roles where rolname = 'kindlast_app'`,
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].rolsuper).toBe(false)
    expect(r.rows[0].rolbypassrls).toBe(false)
  })

  it('kindlast_app owns no tables', async () => {
    const r = await superuser.query(
      `select count(*)::int as n from pg_tables
       where schemaname = 'public' and tableowner = 'kindlast_app'`,
    )
    expect(r.rows[0].n).toBe(0)
  })
})

describe.skipIf(!reachable)('non-vacuity: the superuser path still sees everything', () => {
  it('the superuser reads rows across both orgs', async () => {
    const r = await superuser.query(
      `select count(*)::int as n from findings where org_id in ($1, $2)`,
      [orgA, orgB],
    )
    expect(r.rows[0].n).toBe(2)
  })
})
