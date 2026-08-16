/**
 * A DSAR's statutory clock runs from receipt (ENT-224).
 *
 * `executor_create_dsar_on_approval` used to hardcode `received_at = now()`,
 * so a request that arrived a week ago and was approved today got a deadline a
 * week later than the real one.
 *
 * The direction is what makes it worth a suite. The product would have told a
 * customer they were comfortably on time when they were nearly due, or already
 * late. Under-reporting urgency is the failure a compliance product cannot
 * afford, because the customer stops checking for themselves.
 *
 * The backdated test is the one ENT-224 asks for by name: it fails against the
 * pre-00010 trigger, which is what makes the rest of this suite worth trusting.
 *
 * Refusal is the decision 00010 made on the question ENT-224 left open. A
 * missing receipt date is refused rather than defaulted, because an unknown
 * receipt date means an unknowable deadline and `now()` asserts an optimistic
 * one. Three tests pin that, and they are the ones to change if the decision is
 * ever reversed.
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

const org = randomUUID()
const ada = randomUUID()
const obligationID = randomUUID()
const obligationSlug = `dsar-clock-${randomUUID().slice(0, 8)}`

let migrator: Client
let app: Client
let profile: string

const SUMMARY =
  'A fixture obligation standing in for a real one, long enough to satisfy the ' +
  'hundred character floor the schema places on an obligation summary.'

/** A pending finding whose obligation creates a DSAR, carrying `payload`. */
async function seedDsarFinding(
  payload: Record<string, unknown>,
): Promise<string> {
  const signal = randomUUID()
  const finding = randomUUID()

  await migrator.query(
    `insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
     values ($1, $2, $3, 'dsar', 'Fixture request', $4)`,
    [signal, org, profile, `dedup-${signal}`],
  )
  await migrator.query(
    `insert into findings
       (id, org_id, profile_id, watcher_finding_id, obligation_id, obligation_slug,
        detected, proposed_action, action_type, metadata)
     values ($1, $2, $3, $4, $5, $6, 'A data subject asked for their data',
             'Log and answer it', 'create_dsar', $7)`,
    [
      finding,
      org,
      profile,
      signal,
      obligationID,
      obligationSlug,
      JSON.stringify({ payload }),
    ],
  )
  return finding
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `DSAR Clock Fixture ${org.slice(0, 8)}`],
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

  // A fixture obligation, classified create_dsar. Deliberately NOT one of the
  // real fifteen: 00009 leaves every real obligation unmapped and
  // obligation-classification.test.ts fails if that changes.
  await migrator.query(
    `insert into obligations
       (id, slug, title, summary, citation_celex, citation_kind, citation_article, action_type)
     values ($1, $2, 'Fixture DSAR obligation', $3, '32016R0679', 'article', 12, 'create_dsar')`,
    [obligationID, obligationSlug, SUMMARY],
  )
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id = $1`, [org])
  await migrator.query(`delete from obligations where id = $1`, [obligationID])
  await Promise.all([migrator.end(), app.end()])
})

describe.skipIf(!reachable)('the deadline runs from receipt', () => {
  // The test ENT-224 asks for by name. It fails against the pre-00010 trigger,
  // which hardcoded received_at = now() and would have put the deadline 30 days
  // from approval rather than 30 days from a receipt eight days earlier.
  it('honours a backdated receipt', async () => {
    const received = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000)
    const finding = await seedDsarFinding({
      requester: 'A. Subject',
      request_type: 'access',
      received_at: received.toISOString(),
    })

    await setTenant(app, org, ada)
    await app.query(`select approve_finding($1)`, [finding])

    const r = await migrator.query(
      `select received_at, response_due_at from dsars where finding_id = $1`,
      [finding],
    )
    expect(r.rows).toHaveLength(1)

    const storedReceived = new Date(r.rows[0].received_at).getTime()
    const due = new Date(r.rows[0].response_due_at).getTime()

    // The receipt survived rather than being replaced by now().
    expect(Math.abs(storedReceived - received.getTime())).toBeLessThan(1000)

    // And the deadline is 30 days after THAT, so it has already partly run.
    expect(Math.abs(due - (received.getTime() + 30 * 86400_000))).toBeLessThan(
      1000,
    )

    // Stated the other way round, because this is the property that matters:
    // the deadline is sooner than 30 days from now.
    expect(due).toBeLessThan(Date.now() + 30 * 86400_000)
  })

  it('leaves 30 days when the request arrived just now', async () => {
    const received = new Date()
    const finding = await seedDsarFinding({
      requester: 'B. Subject',
      received_at: received.toISOString(),
    })

    await setTenant(app, org, ada)
    await app.query(`select approve_finding($1)`, [finding])

    const r = await migrator.query(
      `select response_due_at from dsars where finding_id = $1`,
      [finding],
    )
    const due = new Date(r.rows[0].response_due_at).getTime()
    expect(Math.abs(due - (received.getTime() + 30 * 86400_000))).toBeLessThan(
      2000,
    )
  })
})

describe.skipIf(!reachable)('and an unknown receipt date is refused', () => {
  // 00010's decision on the question ENT-224 left open. An unknown receipt date
  // means an unknowable deadline, and now() would assert an optimistic one.
  it('refuses a payload with no received_at', async () => {
    const finding = await seedDsarFinding({ requester: 'C. Subject' })
    await setTenant(app, org, ada)

    await expect(
      app.query(`select approve_finding($1)`, [finding]),
    ).rejects.toThrow(/no received_at/i)
  })

  it('refuses a received_at that is not a timestamp', async () => {
    const finding = await seedDsarFinding({ received_at: 'last Tuesday' })
    await setTenant(app, org, ada)

    await expect(
      app.query(`select approve_finding($1)`, [finding]),
    ).rejects.toThrow(/not a timestamp/i)
  })

  it('refuses a receipt date in the future', async () => {
    // Data entry gone wrong, and it moves the deadline outwards, which is the
    // direction that hides a breach.
    const finding = await seedDsarFinding({
      received_at: new Date(Date.now() + 86400_000).toISOString(),
    })
    await setTenant(app, org, ada)

    await expect(
      app.query(`select approve_finding($1)`, [finding]),
    ).rejects.toThrow(/in the future/i)
  })

  // The refusal is a refusal, not a partial write: the approval is aborted
  // whole, so there is no approved finding with no DSAR behind it.
  it('leaves the finding unapproved when it refuses', async () => {
    const finding = await seedDsarFinding({ requester: 'D. Subject' })
    await setTenant(app, org, ada)

    await expect(
      app.query(`select approve_finding($1)`, [finding]),
    ).rejects.toThrow()

    const r = await migrator.query(
      `select status from findings where id = $1`,
      [finding],
    )
    expect(r.rows[0].status).toBe('pending')

    const audit = await migrator.query(
      `select count(*)::int as n from audit_log where finding_id = $1`,
      [finding],
    )
    expect(audit.rows[0].n).toBe(0)
  })
})
