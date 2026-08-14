/**
 * Personal-organisation stamping, with row counts reconciled before and after
 * (ENT-192 acceptance criterion).
 *
 * The baseline squash is split in two exactly so this is testable and so a
 * production import has a seam to land data in:
 *
 *   00001_baseline.sql       the legacy schema, auth-free (user_id columns
 *                            are plain uuids; Supabase's auth schema is gone)
 *   00002_organisations.sql  the organisation model: org tables, org_id
 *                            everywhere, stamping, NOT NULL, policies, FORCE
 *
 * A fresh deploy runs both back to back. A production import runs 00001,
 * restores a data-only dump, then runs 00002, which stamps every existing
 * user with a personal organisation. This suite simulates the import path on
 * a scratch database: legacy-shaped rows for two users go in between the two
 * migrations, and afterwards every row must belong to its user's personal
 * org, with nothing lost and nothing invented.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { randomUUID } from 'node:crypto'
import { Client } from 'pg'
import {
  connect,
  isStackReachable,
  SUPER_URL,
  MIGRATOR_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const SCRATCH_DB = `stamping_test_${randomUUID().slice(0, 8)}`

function migrationSql(file: string): string {
  const raw = readFileSync(resolve(__dirname, '../migrations', file), 'utf8')
  // Strip goose directives; everything else is plain SQL and the down
  // section of a baseline is empty by design.
  return raw
    .split('\n')
    .filter((line) => !line.trimStart().startsWith('-- +goose'))
    .join('\n')
}

const userOne = randomUUID()
const userTwo = randomUUID()

let admin: Client // superuser on the default db, creates/drops the scratch db
let db: Client // migrator connection to the scratch db

// Legacy-shaped fixture: what a data-only restore from Supabase would land.
async function insertLegacyRows(c: Client): Promise<void> {
  for (const user of [userOne, userTwo]) {
    const session = randomUUID()
    const profile = randomUUID()
    const watcher = randomUUID()
    await c.query(
      `insert into onboarding_sessions (id, user_id, status) values ($1, $2, 'completed')`,
      [session, user],
    )
    await c.query(
      `insert into compliance_profiles
         (id, user_id, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
       values ($1, $2, $3, 'saas', 'no', 'no', 'no')`,
      [profile, user, session],
    )
    await c.query(
      `insert into watcher_findings (id, user_id, profile_id, kind, title, dedup_key)
       values ($1, $2, $3, 'profile_gap', 'stamping fixture', $4)`,
      [watcher, user, profile, `stamping-${watcher}`],
    )
    await c.query(
      `insert into findings
         (user_id, profile_id, watcher_finding_id, obligation_id, detected, proposed_action)
       select $1, $2, $3, o.id, 'stamping fixture', 'none' from obligations o limit 1`,
      [user, profile, watcher],
    )
    await c.query(
      `insert into dsars (user_id, subject_name, response_due_at)
       values ($1, 'Stamping Fixture', now() + interval '30 days')`,
      [user],
    )
    await c.query(
      `insert into subscriptions (user_id, plan, status) values ($1, $2, 'active')`,
      [user, user === userOne ? 'pro' : 'free'],
    )
    await c.query(
      `insert into notification_preferences (user_id) values ($1)`,
      [user],
    )
  }
}

const TENANT_TABLES = [
  'onboarding_sessions',
  'compliance_profiles',
  'watcher_findings',
  'findings',
  'dsars',
  'subscriptions',
  'notification_preferences',
]

const before: Record<string, number> = {}

beforeAll(async () => {
  if (!reachable) return
  admin = await connect(SUPER_URL)
  await admin.query(`create database ${SCRATCH_DB} owner kindlast_migrator`)
  const url = new URL(MIGRATOR_URL)
  url.pathname = `/${SCRATCH_DB}`
  db = await connect(url.toString())
  await db.query(migrationSql('00001_baseline.sql'))
  await insertLegacyRows(db)
  for (const t of TENANT_TABLES) {
    const r = await db.query(`select count(*)::int as n from ${t}`)
    before[t] = r.rows[0].n
  }
  await db.query(migrationSql('00002_organisations.sql'))
})

afterAll(async () => {
  if (!reachable) return
  await db?.end()
  await admin?.query(`drop database if exists ${SCRATCH_DB} (force)`)
  await admin?.end()
})

describe.skipIf(!reachable)('personal organisation stamping', () => {
  it('creates exactly one personal organisation per existing user', async () => {
    const orgs = await db.query(`select count(*)::int as n from organisations`)
    expect(orgs.rows[0].n).toBe(2)
    const owners = await db.query(
      `select user_id, role from memberships order by user_id`,
    )
    expect(owners.rows.map((r) => r.role)).toEqual(['owner', 'owner'])
    expect(new Set(owners.rows.map((r) => r.user_id))).toEqual(
      new Set([userOne, userTwo]),
    )
  })

  it('row counts reconcile before and after on every stamped table', async () => {
    for (const t of TENANT_TABLES) {
      const r = await db.query(`select count(*)::int as n from ${t}`)
      expect(r.rows[0].n, `${t} row count changed during stamping`).toBe(
        before[t],
      )
    }
  })

  it('every row is stamped with its user personal org and org_id is NOT NULL', async () => {
    for (const t of TENANT_TABLES) {
      const nulls = await db.query(
        `select count(*)::int as n from ${t} where org_id is null`,
      )
      expect(nulls.rows[0].n, `${t} has unstamped rows`).toBe(0)
    }
    // Rows created by user one live in the org user one owns, and only there.
    const r = await db.query(
      `select count(*)::int as n
       from dsars d
       join memberships m on m.org_id = d.org_id
       where d.created_by = $1 and m.user_id = $1 and m.role = 'owner'`,
      [userOne],
    )
    expect(r.rows[0].n).toBe(1)
  })

  it('subscriptions move to the organisation and keep their plan', async () => {
    const r = await db.query(
      `select s.plan
       from subscriptions s
       join memberships m on m.org_id = s.org_id
       where m.user_id = $1`,
      [userOne],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].plan).toBe('pro')
    // And subscriptions are keyed by org now: no user_id column remains.
    const cols = await db.query(
      `select column_name from information_schema.columns
       where table_schema = 'public' and table_name = 'subscriptions'`,
    )
    const names = cols.rows.map((c) => c.column_name)
    expect(names).toContain('org_id')
    expect(names).not.toContain('user_id')
  })

  it('user_id means one thing afterwards: tenancy columns are gone, authorship columns remain', async () => {
    const q = async (table: string) => {
      const r = await db.query(
        `select column_name from information_schema.columns
         where table_schema = 'public' and table_name = $1`,
        [table],
      )
      return r.rows.map((c) => c.column_name)
    }
    // Pure-tenancy user_id is dropped.
    expect(await q('findings')).not.toContain('user_id')
    expect(await q('watcher_findings')).not.toContain('user_id')
    // Authored records carry created_by instead.
    for (const t of ['dsars', 'compliance_profiles', 'onboarding_sessions']) {
      const cols = await q(t)
      expect(cols, `${t} should have created_by`).toContain('created_by')
      expect(cols, `${t} should not have user_id`).not.toContain('user_id')
    }
    // The audit log keeps its actor and snapshots the actor's role.
    const audit = await q('audit_log')
    expect(audit).toContain('user_id')
    expect(audit).toContain('actor_role')
    expect(audit).toContain('org_id')
  })
})
