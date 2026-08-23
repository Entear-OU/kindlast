/**
 * A signal says what produced it, and cannot change hands (ENT-273).
 *
 * The deduplication accident this came from is worth stating, because the
 * tests below only make sense against it. `emit_watcher_finding` upserts on
 * `(profile_id, dedup_key) where status = 'open'`, so whoever writes a key
 * owns the row it lands on. The agentic Watcher is shown every open signal
 * with its key, because a run that is not told what is already open repeats
 * it. A model that echoes a key back therefore does not raise a duplicate, it
 * overwrites the detector's row, and `scripts/watcher-comparison.py` caught
 * exactly that the first time it ran: "Profile gap: Records of Processing
 * Activities" retitled by a model and dropped from high severity to medium.
 *
 * PR #244 fixed it in Go by namespacing agent keys. This suite is about the
 * half that fix could not give: that the property holds for the NEXT writer,
 * including one that bypasses `emit_watcher_finding` entirely.
 *
 * Proven able to fail: `goose down` to 00038 turns every test in the second
 * and third blocks red, the takeover cases by silently succeeding.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import { connect, isStackReachable, roleUrl, MIGRATOR_URL } from './helpers/db'

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

const org = randomUUID()
const ada = randomUUID()

let migrator: Client
let agent: Client
let profile: string

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  agent = await connect(AGENT_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `Signal Source ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )
  const session = randomUUID()
  profile = randomUUID()
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

  await agent.query(`select set_config('app.current_org_id', $1, false)`, [org])
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id = $1`, [org])
  await Promise.all([migrator.end(), agent.end()])
})

/** Raises a signal through the one writer, as the producer role. */
async function emit(
  key: string,
  overrides: {
    title?: string
    severity?: string
    source?: string
  } = {},
): Promise<string> {
  const r = await agent.query(
    `select emit_watcher_finding($1::uuid, 'profile_gap', $2, $3, null, $4, null, '{}'::jsonb, $5)::text as id`,
    [
      profile,
      key,
      overrides.title ?? 'A signal',
      overrides.severity ?? 'high',
      overrides.source ?? 'detector',
    ],
  )
  return r.rows[0].id
}

async function sourceOf(id: string): Promise<string> {
  const r = await migrator.query(
    `select source from watcher_findings where id = $1`,
    [id],
  )
  return r.rows[0].source
}

describe.skipIf(!reachable)('a signal records what produced it', () => {
  it('defaults to detector, so every existing caller keeps meaning what it meant', async () => {
    // `run_watcher`'s detectors pass eight arguments and know nothing about
    // this column. That has to keep working, and has to keep being right.
    const r = await agent.query(
      `select emit_watcher_finding($1::uuid, 'profile_gap', $2, 'Default source', null, 'high', null, '{}'::jsonb)::text as id`,
      [profile, `detector-default-${randomUUID()}`],
    )
    expect(await sourceOf(r.rows[0].id)).toBe('detector')
  })

  it('exists exactly once, so an eight-argument call is not ambiguous', async () => {
    // THE BUG THIS MIGRATION HAD, CAUGHT BY THE TEST ABOVE.
    //
    // `create or replace function` matches on the argument list. Adding
    // `p_source` with a default therefore created an OVERLOAD rather than
    // replacing anything, and the eight-argument original survived. Every
    // detector call then failed with "function emit_watcher_finding(...) is
    // not unique", because Postgres could not choose between the eight
    // argument function and the nine argument one whose ninth has a default.
    //
    // The test above catches it today. This catches it when somebody adds a
    // tenth parameter in two years and reaches for `create or replace` again,
    // which is the more likely way it comes back.
    const r = await migrator.query(
      `select count(*)::int as n
         from pg_proc p join pg_namespace n on n.oid = p.pronamespace
        where n.nspname = 'public' and p.proname = 'emit_watcher_finding'`,
    )
    expect(r.rows[0].n).toBe(1)
  })

  it('records agent when the caller says so', async () => {
    const id = await emit(`agent-said-${randomUUID()}`, { source: 'agent' })
    expect(await sourceOf(id)).toBe('agent')
  })

  it('refuses a source nobody has defined', async () => {
    // The vocabulary is two, and a third should arrive with a migration and a
    // reason rather than by a caller inventing one.
    await expect(
      emit(`unknown-${randomUUID()}`, { source: 'intern' }),
    ).rejects.toThrow(/watcher_findings_source_known|check constraint/i)
  })
})

describe.skipIf(!reachable)('and a signal cannot change hands', () => {
  it('refuses an agent taking over a row a detector owns', async () => {
    // The observed failure, reduced to its shape: same profile, same key, and
    // the second writer is not the first.
    const key = `takeover-${randomUUID()}`
    const id = await emit(key, {
      title: 'Profile gap: Records of Processing Activities',
      severity: 'high',
      source: 'detector',
    })

    await expect(
      emit(key, {
        title: 'Rewritten by a model',
        severity: 'medium',
        source: 'agent',
      }),
    ).rejects.toThrow(/cannot be taken over/i)

    // And the refusal left the detector's row exactly as it was, rather than
    // half-applying. This is the assertion that matters to a customer: the
    // severity they were shown is the severity the rule produced.
    const after = await migrator.query(
      `select title, severity, source from watcher_findings where id = $1`,
      [id],
    )
    expect(after.rows[0].title).toBe(
      'Profile gap: Records of Processing Activities',
    )
    expect(after.rows[0].severity).toBe('high')
    expect(after.rows[0].source).toBe('detector')
  })

  it('refuses a detector taking over a row an agent owns', async () => {
    // The reverse, which is not symmetry for its own sake: a detector whose
    // key generation changed could land on an agent's row and silently present
    // a model's words as a rule's output, which is the worse direction of the
    // two.
    const key = `reverse-${randomUUID()}`
    await emit(key, { source: 'agent' })

    await expect(emit(key, { source: 'detector' })).rejects.toThrow(
      /cannot be taken over/i,
    )
  })

  it('lets a writer update its own row, which is the ordinary daily sweep', async () => {
    // The guard is only worth having if it can pass. A detector re-running
    // against a condition that is still true must update in place, or every
    // sweep would produce a new row.
    const key = `same-hands-${randomUUID()}`
    const first = await emit(key, { title: 'First look', severity: 'medium' })
    const second = await emit(key, { title: 'Second look', severity: 'high' })

    expect(second).toBe(first)
    const after = await migrator.query(
      `select title, severity from watcher_findings where id = $1`,
      [first],
    )
    expect(after.rows[0].title).toBe('Second look')
    expect(after.rows[0].severity).toBe('high')
  })

  it('refuses a takeover written as a plain update, not only through the function', async () => {
    // THE REASON THIS IS A TRIGGER AND NOT A CHECK INSIDE THE FUNCTION.
    //
    // `emit_watcher_finding` is the one writer today. A guard that lives
    // inside it is a guard the next writer has to remember, which is what the
    // Go prefix already was. This asserts the property holds against a caller
    // that never calls the function at all.
    const key = `direct-${randomUUID()}`
    const id = await emit(key, { source: 'detector' })

    await expect(
      agent.query(
        `update watcher_findings set source = 'agent' where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/cannot be taken over/i)
  })
})
