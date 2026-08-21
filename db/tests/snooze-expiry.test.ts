/**
 * Who may bring a deferred finding back (ENT-256, part two, migration 00034).
 *
 * `expire_snoozed_findings()` is the eighth SECURITY DEFINER function, and the
 * argument for it is in 00034: a maintenance pass over every organisation at
 * once, started by a schedule with no tenant and no person, which no
 * single-organisation policy can express. A definer function is how RLS gets
 * bypassed by accident, so the two things worth proving here are the two that
 * would make this one an accident:
 *
 *   1. Only the producer role may execute it. The application role serves
 *      requests and does not get to bring a deferred decision back early; a
 *      grant to PUBLIC (which is what 00002 left) would have let it.
 *   2. It reaches findings in every organisation with no GUC set, because that
 *      is its job, and it moves nothing that is not due.
 *
 * Proven able to fail: revoking the agent's execute turns the first group red
 * with "permission denied for function", and making the function SECURITY
 * INVOKER again turns the second red with the GUC error, which is exactly the
 * failure this migration exists to remove.
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

const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()

let migrator: Client
let agent: Client
let app: Client

/** An organisation with one member and one profile, as the migrator. */
async function seedOrg(org: string, label: string): Promise<string> {
  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `${label} ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )
  const session = randomUUID()
  const profile = randomUUID()
  await migrator.query(
    `insert into onboarding_sessions (id, org_id, created_by) values ($1, $2, $3)`,
    [session, org, ada],
  )
  await migrator.query(
    `insert into compliance_profiles
       (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
     values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')`,
    [profile, org, ada, session],
  )
  return profile
}

/** A finding deferred until `untilSql`, an SQL expression in the DB's clock. */
async function seedSnoozed(
  org: string,
  profile: string,
  untilSql: string,
): Promise<string> {
  const id = randomUUID()
  const signal = randomUUID()
  await migrator.query(
    `insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
     values ($1, $2, $3, 'profile_gap', 'expiry fixture', $4)`,
    [signal, org, profile, `expiry-${signal}`],
  )
  await migrator.query(
    `insert into findings (id, org_id, profile_id, watcher_finding_id, obligation_id,
                           detected, proposed_action, status, snoozed_until)
     select $1, $2, $3, $4, o.id, 'expiry fixture', 'nothing', 'snoozed', ${untilSql}
       from obligations o limit 1`,
    [id, org, profile, signal],
  )
  return id
}

async function statusOf(id: string): Promise<string> {
  const r = await migrator.query(`select status from findings where id = $1`, [
    id,
  ])
  return r.rows[0].status
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  agent = await connect(AGENT_URL)
  app = await connect(APP_URL)
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    orgA,
    orgB,
  ])
  await Promise.all([migrator.end(), agent.end(), app.end()])
})

describe.skipIf(!reachable)('only the producer may expire snoozes', () => {
  it('the application role cannot execute it', async () => {
    // GUCs set, so this is a privilege failure rather than a policy one.
    await app.query(`select set_config('app.current_org_id', $1, false)`, [
      orgA,
    ])
    await app.query(`select set_config('app.current_user_id', $1, false)`, [
      ada,
    ])
    await expect(
      app.query(`select public.expire_snoozed_findings()`),
    ).rejects.toThrow(/permission denied for function/)
  })

  it('the producer role can', async () => {
    const r = await agent.query(
      `select public.expire_snoozed_findings()::int as n`,
    )
    expect(r.rows[0].n).toBeGreaterThanOrEqual(0)
  })
})

describe.skipIf(!reachable)(
  'and it reaches every organisation with no tenant set',
  () => {
    it('brings back what is due in two organisations and leaves the rest', async () => {
      const profileA = await seedOrg(orgA, 'Expiry A')
      const profileB = await seedOrg(orgB, 'Expiry B')
      const dueA = await seedSnoozed(
        orgA,
        profileA,
        `now() - interval '1 hour'`,
      )
      const dueB = await seedSnoozed(orgB, profileB, `now() - interval '1 day'`)
      const notYet = await seedSnoozed(
        orgA,
        profileA,
        `now() + interval '1 day'`,
      )

      // No set_config on the agent connection: a schedule has no tenant to
      // name, and the whole point of 00034 is that this works anyway.
      const r = await agent.query(
        `select public.expire_snoozed_findings()::int as n`,
      )
      expect(r.rows[0].n).toBeGreaterThanOrEqual(2)

      expect(await statusOf(dueA)).toBe('pending')
      expect(await statusOf(dueB)).toBe('pending')
      expect(await statusOf(notYet)).toBe('snoozed')
    })
  },
)
