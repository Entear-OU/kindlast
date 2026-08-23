/**
 * The two things a scheduled sweep needs from the database (ENT-256, part
 * four, migration 00035): the `sweep_triggers` table that turns "confirm
 * onboarding" into a sweep, and `sweep_targets()`, the ninth SECURITY DEFINER
 * function, which is how the daily workflow learns which organisations exist
 * without the producer role being allowed to enumerate tenants.
 *
 * A definer function is how RLS gets bypassed by accident, so the two things
 * worth proving are the two that would make this one an accident:
 *
 *   1. Only the producer role may execute it. The application role serves
 *      requests and must not be handed a list of every tenant.
 *   2. It returns exactly the organisations with a compliance profile, from a
 *      connection with no GUC set, and nothing about them but the id.
 *
 * And for the table, the grant split 00035 draws: the application enqueues
 * inside its own organisation and cannot mark a row done; the agent lists and
 * marks across every organisation and cannot enqueue.
 *
 * Proven able to fail: revoking the agent's execute on `sweep_targets()` turns
 * the first group red with "permission denied for function"; making it
 * SECURITY INVOKER turns the second red with an empty list, because the
 * agent's policies on `compliance_profiles` see nothing with no GUC set.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  roleUrl,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const AGENT_URL = roleUrl('agent')

const reachable = (await isStackReachable()) && (await agentReachable())

async function agentReachable(): Promise<boolean> {
  try {
    const client = await connect(AGENT_URL)
    await client.end()
    return true
  } catch {
    return false
  }
}

const withProfile = randomUUID()
const withoutProfile = randomUUID()
const ada = randomUUID()

let migrator: Client
let agent: Client
let app: Client

async function seedOrg(org: string, label: string, profile: boolean) {
  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `${label} ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )
  if (!profile) return
  const session = randomUUID()
  await migrator.query(
    `insert into onboarding_sessions (id, org_id, created_by) values ($1, $2, $3)`,
    [session, org, ada],
  )
  await migrator.query(
    `insert into compliance_profiles
       (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
     values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')`,
    [randomUUID(), org, ada, session],
  )
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  agent = await connect(AGENT_URL)
  app = await connect(APP_URL)
  await seedOrg(withProfile, 'Sweep target', true)
  await seedOrg(withoutProfile, 'Never onboarded', false)
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    withProfile,
    withoutProfile,
  ])
  await Promise.all([migrator.end(), agent.end(), app.end()])
})

describe.skipIf(!reachable)('only the producer may list sweep targets', () => {
  it('the application role cannot execute it', async () => {
    await app.query(`select set_config('app.current_org_id', $1, false)`, [
      withProfile,
    ])
    await app.query(`select set_config('app.current_user_id', $1, false)`, [
      ada,
    ])
    await expect(app.query(`select public.sweep_targets()`)).rejects.toThrow(
      /permission denied for function/,
    )
  })

  it('the producer role can, with no tenant set, and gets the organisations with a profile', async () => {
    const r = await agent.query(`select sweep_targets()::text as org_id`)
    const ids = r.rows.map((row: { org_id: string }) => row.org_id)
    expect(ids).toContain(withProfile)
    expect(ids).not.toContain(withoutProfile)
    // Ids and nothing else: one column, no name, no slug, no member.
    expect(r.fields.map((f) => f.name)).toEqual(['org_id'])
  })
})

describe.skipIf(!reachable)(
  'sweep_triggers holds the split 00035 draws',
  () => {
    it('the application enqueues in its own organisation and cannot mark done', async () => {
      await app.query(`select set_config('app.current_org_id', $1, false)`, [
        withProfile,
      ])
      await app.query(`select set_config('app.current_user_id', $1, false)`, [
        ada,
      ])
      await app.query(
        `insert into sweep_triggers (org_id, reason) values ($1, 'onboarding_confirmed')`,
        [withProfile],
      )
      // Another organisation's row is refused by the insert policy.
      await expect(
        app.query(
          `insert into sweep_triggers (org_id, reason) values ($1, 'onboarding_confirmed')`,
          [withoutProfile],
        ),
      ).rejects.toThrow(/row-level security/)
      // No update grant at all: the role that enqueues does not mark done.
      await expect(
        app.query(`update sweep_triggers set status = 'done', done_at = now()`),
      ).rejects.toThrow(/permission denied/)
    })

    it('the producer lists and marks across organisations and cannot enqueue', async () => {
      const listed = await agent.query(
        `select id, org_id::text as org_id from sweep_triggers where status = 'pending' and org_id = $1`,
        [withProfile],
      )
      expect(listed.rows.length).toBe(1)
      const id = listed.rows[0].id
      await agent.query(
        `update sweep_triggers set status = 'done', done_at = now(), attempts = attempts + 1 where id = $1`,
        [id],
      )
      const after = await migrator.query(
        `select status from sweep_triggers where id = $1`,
        [id],
      )
      expect(after.rows[0].status).toBe('done')
      await expect(
        agent.query(
          `insert into sweep_triggers (org_id, reason) values ($1, 'onboarding_confirmed')`,
          [withProfile],
        ),
      ).rejects.toThrow(/permission denied/)
    })
  },
)
