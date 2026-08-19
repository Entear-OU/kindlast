/**
 * The producer role's boundary (ENT-203).
 *
 * `kindlast_agent` exists because `kindlast_app` deliberately cannot create
 * findings, so the Watcher and the Analyst cannot run as the application. That
 * is 00002's design working rather than a gap: the thing that serves requests
 * should not be able to fabricate a claim about a customer's legal exposure.
 *
 * The agent's policies are the only ones in this database that omit the
 * membership half of the two-GUC form, because a sweep is started by the system
 * and there is no member to check. That makes this suite the one place where
 * "org equality alone is still tenancy" has to be demonstrated rather than
 * assumed, and it is why the cross-organisation tests here matter more than the
 * happy-path ones.
 *
 * It also pins what the agent must NOT be able to do. A role that can invent a
 * finding must not be able to approve one, read the audit log, or see who the
 * members are. Those tests fail if a later grant is written generously.
 *
 * Proven able to fail: dropping the `org_id = ...` predicate from
 * `watcher_findings_agent` turns the cross-organisation tests red while leaving
 * the happy path green, which is the shape that says they test tenancy rather
 * than connectivity.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  setTenant,
  isStackReachable,
  roleUrl,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const AGENT_URL = roleUrl('agent')

// The agent role arrives with 00008 and an operator step. A stack that predates
// it should skip rather than fail, the same way an absent stack does.
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
let profileA: string
let profileB: string

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

/** Points the agent at one organisation. No user GUC: there is no user. */
async function pointAt(org: string): Promise<void> {
  await agent.query(`select set_config('app.current_org_id', $1, false)`, [org])
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  agent = await connect(AGENT_URL)

  profileA = await seedOrg(orgA, 'Agent Fixture A')
  profileB = await seedOrg(orgB, 'Agent Fixture B')
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    orgA,
    orgB,
  ])
  await Promise.all([migrator.end(), agent.end()])
})

describe.skipIf(!reachable)(
  'the agent can produce, within one organisation',
  () => {
    it('inserts a signal without any membership', async () => {
      await pointAt(orgA)

      const r = await agent.query(
        `insert into watcher_findings (org_id, profile_id, kind, title, dedup_key)
       values ($1, $2, 'profile_gap', 'Agent signal', $3)
       returning id`,
        [orgA, profileA, `agent-${randomUUID()}`],
      )

      expect(r.rows).toHaveLength(1)
    })

    it('stamps watcher_last_run_at on the profile it swept', async () => {
      await pointAt(orgA)

      await agent.query(
        `update compliance_profiles set watcher_last_run_at = now() where id = $1`,
        [profileA],
      )

      const r = await migrator.query(
        `select watcher_last_run_at from compliance_profiles where id = $1`,
        [profileA],
      )
      expect(r.rows[0].watcher_last_run_at).not.toBeNull()
    })

    it('reads the obligations it needs to cite', async () => {
      await pointAt(orgA)
      // The corpus is the law, not tenant data: the same rows for every customer,
      // carrying no org_id. A count is enough; the point is that it is readable.
      const r = await agent.query(`select count(*)::int as n from obligations`)
      expect(r.rows[0].n).toBeGreaterThanOrEqual(0)
    })

    // The test the rest of this block cannot give, and the one 00032 exists
    // because nobody had written.
    //
    // Every test above names a table and proves the agent may touch it, which
    // proves the grants somebody remembered rather than the grants the sweep
    // needs. `run_watcher` calls three detectors, two of which read `dsars`,
    // and no grant on `dsars` was ever issued: the suite stayed green for as
    // long as the Watcher was completely unable to run, because nothing here
    // ever called it.
    //
    // So this calls the real entry point on the real role. It asserts almost
    // nothing about the result, deliberately: what it is guarding is that the
    // function reaches its end without a 42501, and the next detector to read
    // a table nobody granted turns this red on the commit that adds it rather
    // than on the day a customer notices an empty feed.
    //
    // Proven able to fail: `revoke select on public.dsars from kindlast_agent`
    // makes this the only red test in the suite, with the same
    // "permission denied for table dsars" the running stack returned.
    it('completes a whole sweep as itself, not just the reads it was granted', async () => {
      await pointAt(orgA)

      const r = await agent.query(`select public.run_watcher() as swept`)

      // A count of profiles swept. Zero would mean the loop found nothing and
      // the detectors never ran, which would make this pass without testing
      // anything: the fixtures put a profile in orgA precisely so it cannot.
      expect(r.rows[0].swept).toBeGreaterThan(0)
    })
  },
)

describe.skipIf(!reachable)('and org equality alone is still tenancy', () => {
  // The test this whole suite exists for. The agent's policies drop the
  // membership check, so this is the only thing standing between a sweep
  // pointed at one organisation and another's data.
  it('cannot write a signal into an organisation it was not pointed at', async () => {
    await pointAt(orgA)

    await expect(
      agent.query(
        `insert into watcher_findings (org_id, profile_id, kind, title, dedup_key)
         values ($1, $2, 'profile_gap', 'Cross-org signal', $3)`,
        [orgB, profileB, `agent-${randomUUID()}`],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('cannot read another organisation’s findings', async () => {
    const finding = randomUUID()
    const signal = randomUUID()
    await migrator.query(
      `insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
       values ($1, $2, $3, 'profile_gap', 'B signal', $4)`,
      [signal, orgB, profileB, `b-${signal}`],
    )
    await migrator.query(
      `insert into findings (id, org_id, profile_id, watcher_finding_id, obligation_id,
                             detected, proposed_action)
       select $1, $2, $3, $4, o.id, 'B finding', 'B action'
         from obligations o limit 1`,
      [finding, orgB, profileB, signal],
    )

    await pointAt(orgA)
    const r = await agent.query(
      `select count(*)::int as n from findings where id = $1`,
      [finding],
    )
    expect(r.rows[0].n).toBe(0)
  })

  it('cannot update another organisation’s profile', async () => {
    await pointAt(orgA)

    const r = await agent.query(
      `update compliance_profiles set watcher_last_run_at = now() where id = $1`,
      [profileB],
    )
    // No error, no rows: the policy makes the row invisible rather than
    // refusing the statement, which is the same shape every other tenant table
    // has.
    expect(r.rowCount).toBe(0)
  })
})

describe.skipIf(!reachable)('and it can produce but not decide', () => {
  // A role that can invent a finding must not be able to approve one. These
  // fail if a later grant is written generously.
  it('cannot approve a finding', async () => {
    await pointAt(orgA)
    await expect(
      agent.query(`select approve_finding($1)`, [randomUUID()]),
    ).rejects.toThrow()
  })

  it('cannot read the audit log', async () => {
    await pointAt(orgA)
    await expect(agent.query(`select count(*) from audit_log`)).rejects.toThrow(
      /permission denied/i,
    )
  })

  it('cannot read who the members are', async () => {
    await pointAt(orgA)
    await expect(
      agent.query(`select count(*) from memberships`),
    ).rejects.toThrow(/permission denied/i)
  })

  it('cannot read the organisations table', async () => {
    await pointAt(orgA)
    await expect(
      agent.query(`select count(*) from organisations`),
    ).rejects.toThrow(/permission denied/i)
  })
})

describe.skipIf(!reachable)('and the application still cannot produce', () => {
  // The property 00008 must not have weakened. If this ever passes, the
  // separation the agent role exists for has been undone somewhere else.
  //
  // It used to be refused by row level security: 00002 granted the app insert
  // on every table, so the write parsed, found no policy admitting it and
  // touched nothing. 00029 took the grant away, so the refusal is now a 42501
  // at parse time instead. Same property, one layer earlier and considerably
  // louder, which is the whole point of ENT-243. Both spellings are accepted
  // here because either one is the separation holding.
  it('kindlast_app still cannot insert a signal', async () => {
    const app = await connect(APP_URL)
    try {
      await setTenant(app, orgA, ada)
      await expect(
        app.query(
          `insert into watcher_findings (org_id, profile_id, kind, title, dedup_key)
           values ($1, $2, 'profile_gap', 'App signal', $3)`,
          [orgA, profileA, `app-${randomUUID()}`],
        ),
      ).rejects.toThrow(/permission denied|row-level security/i)
    } finally {
      await app.end()
    }
  })
})
