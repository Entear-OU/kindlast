/**
 * The act path writes the audit row (ENT-203).
 *
 * ENT-203's first acceptance criterion is that approving a finding writes
 * exactly one `audit_log` row carrying the acting user and their role at the
 * time. Before 00006 it wrote none: neither `approve_finding` nor
 * `reject_finding` called `record_audit_log`, and the only call sites an
 * approval could reach were inside executor triggers gated on an `action_type`
 * that `analyst_convert_signal` never sets (ENT-165).
 *
 * So this suite is not decoration over a working path. Every test here failed
 * before 00006, which is the only reason to trust the ones that assert a
 * count of exactly one.
 *
 * Proven able to fail, per AGENTS.md, rather than assumed. Three breakages
 * were applied to the live database and the results measured:
 *
 *   * Removing the `perform public.record_audit_log(...)` call from
 *     `approve_finding` turns the four `approving a finding` tests red.
 *   * Widening `approve_finding`'s target_id lookup back to the 00002 form
 *     (no `target_table <> 'findings'` filter) turns exactly one test red,
 *     "does not mistake its own decision row for a created record", and leaves
 *     the other ten green. That is the one worth noting: it says the suite
 *     distinguishes "an audit row exists" from "the right row was read".
 *   * Rolling the whole migration back with `goose down` turns seven red.
 *
 * The four that survive a full rollback are the three cross-organisation tests
 * and the target_id one, all of which assert an absence. An absence is equally
 * true when the feature is missing, which is why they are not evidence on their
 * own and are paired with the counts above.
 *
 * The role snapshot deserves its own note. A `viewer` can approve at the
 * database level, because every policy here is an org equality plus a
 * membership check and none of them read the role: role is a handler concern
 * (§0.5), and RLS deliberately does not express it. That is exactly why the
 * snapshot is worth asserting. If a handler check is ever missed, the audit
 * trail is what records that a viewer approved it.
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

// Two organisations that share nothing. Ada owns the first, Miko views it, and
// Bob owns the second and exists to be refused.
const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()
const miko = randomUUID()
const bob = randomUUID()

const obligationID = randomUUID()
const obligationSlug = `act-path-audit-${randomUUID().slice(0, 8)}`

let migrator: Client
let app: Client

/** A finding in `org`, pending, with the whole chain it needs underneath it. */
async function seedFinding(org: string, actor: string): Promise<string> {
  const session = randomUUID()
  const profile = randomUUID()
  const signal = randomUUID()
  const finding = randomUUID()

  // `created_by` rather than `user_id`: 00002 split the column that meant two
  // things, and on this table it meant authorship.
  await migrator.query(
    `insert into onboarding_sessions (id, org_id, created_by) values ($1, $2, $3)`,
    [session, org, actor],
  )
  await migrator.query(
    `insert into compliance_profiles
       (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
     values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')`,
    [profile, org, actor, session],
  )
  await migrator.query(
    `insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
     values ($1, $2, $3, 'profile_gap', 'Fixture signal', $4)`,
    [signal, org, profile, `dedup-${signal}`],
  )
  await migrator.query(
    `insert into findings
       (id, org_id, profile_id, watcher_finding_id, obligation_id, obligation_slug,
        detected, proposed_action)
     values ($1, $2, $3, $4, $5, $6, 'Fixture gap', 'Fixture action')`,
    [finding, org, profile, signal, obligationID, obligationSlug],
  )

  return finding
}

/** Audit rows for a finding, newest last, read as the migrator so RLS is not
 *  what is under test here. */
async function auditRows(finding: string) {
  const r = await migrator.query(
    `select user_id, actor_role, action_type, target_table, target_id, before, after
       from audit_log
      where finding_id = $1
      order by occurred_at asc`,
    [finding],
  )
  return r.rows
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  for (const [org, label] of [
    [orgA, 'Act Path Fixture A'],
    [orgB, 'Act Path Fixture B'],
  ] as const) {
    await migrator.query(
      `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
      [org, `${label} ${org.slice(0, 8)}`],
    )
  }

  await migrator.query(
    `insert into memberships (org_id, user_id, role) values
       ($1, $2, 'owner'), ($1, $3, 'viewer'), ($4, $5, 'owner')`,
    [orgA, ada, miko, orgB, bob],
  )

  // One obligation shared by every fixture finding. Two constraints shape this
  // row rather than the fixture author's taste: the citation columns must agree
  // with citation_kind, so an article citation carries an article number and
  // neither a recital nor an annex; and summary is held to at least 100
  // characters, which is the schema refusing to let an obligation exist without
  // a usable plain-language statement of what it requires.
  await migrator.query(
    `insert into obligations
       (id, slug, title, summary, citation_celex, citation_kind, citation_article)
     values ($1, $2, 'Fixture obligation', $3, '32016R0679', 'article', 30)`,
    [
      obligationID,
      obligationSlug,
      'A fixture obligation standing in for a real one, long enough to satisfy the ' +
        'hundred character floor the schema places on an obligation summary.',
    ],
  )
})

afterAll(async () => {
  if (!reachable) return
  // organisations cascades to profiles, signals and findings; audit_log rows
  // and the obligation are not org-cascaded, so they go explicitly.
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    orgA,
    orgB,
  ])
  await migrator.query(`delete from obligations where id = $1`, [obligationID])
  await Promise.all([migrator.end(), app.end()])
})

describe.skipIf(!reachable)('approving a finding', () => {
  it('writes exactly one audit row, with the actor and their role', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    await app.query(`select approve_finding($1)`, [finding])

    const rows = await auditRows(finding)
    expect(rows).toHaveLength(1)
    expect(rows[0].user_id).toBe(ada)
    expect(rows[0].actor_role).toBe('owner')
    expect(rows[0].action_type).toBe('approve_finding')
    expect(rows[0].target_table).toBe('findings')
  })

  it('snapshots the role the actor held, not the role they might hold', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, miko)

    await app.query(`select approve_finding($1)`, [finding])

    const rows = await auditRows(finding)
    expect(rows).toHaveLength(1)
    expect(rows[0].user_id).toBe(miko)
    expect(rows[0].actor_role).toBe('viewer')
  })

  it('records what changed, so the row can be checked rather than trusted', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    await app.query(`select approve_finding($1)`, [finding])

    const [row] = await auditRows(finding)
    expect(row.before.status).toBe('pending')
    expect(row.after.status).toBe('approved')
    expect(row.after.approved_by).toBe(ada)
  })

  it('is idempotent: approving twice does not write a second row', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    await app.query(`select approve_finding($1)`, [finding])
    await app.query(`select approve_finding($1)`, [finding])

    expect(await auditRows(finding)).toHaveLength(1)
  })

  // The 00002 lookup ordered by occurred_at over every row for the finding.
  // Now that the decision itself is a row, that form would return the finding's
  // own id and the console would navigate to the finding the founder is already
  // looking at. With no executor trigger reachable yet (ENT-165) the honest
  // answer is null.
  it('does not mistake its own decision row for a created record', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    const r = await app.query(`select approve_finding($1) as target`, [finding])
    expect(r.rows[0].target).toBeNull()
  })
})

describe.skipIf(!reachable)('rejecting and snoozing', () => {
  it('reject writes exactly one row, naming the act', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    await app.query(`select reject_finding($1, $2)`, [
      finding,
      'Not applicable',
    ])

    const rows = await auditRows(finding)
    expect(rows).toHaveLength(1)
    expect(rows[0].action_type).toBe('reject_finding')
    expect(rows[0].after.rejection_reason).toBe('Not applicable')
  })

  it('reject is idempotent', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    await app.query(`select reject_finding($1, $2)`, [finding, 'first'])
    await app.query(`select reject_finding($1, $2)`, [finding, 'second'])

    const rows = await auditRows(finding)
    expect(rows).toHaveLength(1)
    expect(rows[0].after.rejection_reason).toBe('first')
  })

  // Deliberately not idempotent, and pinned so that a later "tidy-up" has to
  // argue with a test. Deferring an already-deferred finding is a second
  // decision with a new date, and an audit trail that recorded only the first
  // would say a finding was deferred once when a human deferred it twice.
  it('snooze records every deferral, because each one is a decision', async () => {
    const finding = await seedFinding(orgA, ada)
    await setTenant(app, orgA, ada)

    await app.query(`select snooze_finding($1, 7)`, [finding])
    await app.query(`select snooze_finding($1, 30)`, [finding])

    const rows = await auditRows(finding)
    expect(rows).toHaveLength(2)
    expect(rows.map((r) => r.action_type)).toEqual([
      'snooze_finding',
      'snooze_finding',
    ])
  })
})

describe.skipIf(!reachable)('and none of it crosses an organisation', () => {
  it('cannot approve a finding belonging to another organisation', async () => {
    const finding = await seedFinding(orgB, bob)
    await setTenant(app, orgA, ada)

    const r = await app.query(`select approve_finding($1) as target`, [finding])

    expect(r.rows[0].target).toBeNull()
    expect(await auditRows(finding)).toHaveLength(0)

    // And the refusal is a refusal, not a silent partial write: the finding is
    // untouched.
    const state = await migrator.query(
      `select status from findings where id = $1`,
      [finding],
    )
    expect(state.rows[0].status).toBe('pending')
  })

  it('cannot reject or snooze one either', async () => {
    const finding = await seedFinding(orgB, bob)
    await setTenant(app, orgA, ada)

    const rejected = await app.query(`select reject_finding($1) as ok`, [
      finding,
    ])
    const snoozed = await app.query(`select snooze_finding($1) as until`, [
      finding,
    ])

    expect(rejected.rows[0].ok).toBe(false)
    expect(snoozed.rows[0].until).toBeNull()
    expect(await auditRows(finding)).toHaveLength(0)
  })

  it('an unknown finding is refused the same way as another org’s', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(`select approve_finding($1) as target`, [
      randomUUID(),
    ])
    expect(r.rows[0].target).toBeNull()
  })
})
