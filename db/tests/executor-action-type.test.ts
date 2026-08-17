/**
 * The Executor fires (ENT-165, inside ENT-203).
 *
 * `findings.action_type` was read by three triggers and written by nothing:
 * `analyst_convert_signal`'s INSERT column list omitted it, so every finding
 * got the column default `'review'` and none of the three executor triggers
 * could ever fire. Approving a finding changed a status and did nothing else,
 * while the billing page sold "One-tap Executor actions".
 *
 * 00007 puts the action on the obligation, where the knowledge is: what
 * approving should *do* is a property of the regulatory requirement, not of
 * whichever sweep happened to notice it.
 *
 * The test worth reading is "returns the id of the record it created". That is
 * the first time `approve_finding`'s return value has ever been non-null, and
 * it closes the loop 00006 opened: the decision row and the creation row are
 * separate, and the lookup has to find the second while ignoring the first.
 *
 * Proven able to fail: dropping `action_type` from 00007's INSERT column list
 * (i.e. restoring the ENT-165 bug) turns the five `an obligation that creates a
 * record` tests red and leaves the `review` ones green, which is the right
 * shape. Reverting 00006's `target_table <> 'findings'` filter turns only
 * "returns the id of the record it created" red.
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

/**
 * Approve a finding exactly as core-api does (ENT-225).
 *
 * `approve_finding` decided things, so it moved to Go and 00016 dropped it.
 * This is the statement the Go store now issues.
 *
 * The subject of this file is unchanged: the three Executor triggers are still
 * SQL, still fire on `after update of status`, and are still what is being
 * tested. Only the thing pulling the trigger moved, and driving them with the
 * real UPDATE is closer to production than calling a wrapper was.
 */
const APPROVE = `
  update findings
     set status = 'approved',
         approved_by = current_setting('app.current_user_id')::uuid,
         approval_reviewed = false
   where id = $1
     and org_id = current_setting('app.current_org_id')::uuid
     and status <> 'approved'
`

/**
 * The decision audit row, which the Go store writes after the UPDATE.
 *
 * Included in the helper below rather than dropped from these tests, because
 * "two rows for two facts" is the property this file documents and it is still
 * true; what changed is that the two facts are now written by two layers. The
 * trigger's creation row is the half this file actually tests, and asserting it
 * alongside a decision row the helper produced still proves the trigger fired.
 */
const RECORD_DECISION = `
  select record_audit_log(
    current_setting('app.current_org_id')::uuid,
    current_setting('app.current_user_id')::uuid,
    $1, $2, 'findings', $1, null, to_jsonb(f.*),
    current_setting('app.current_user_id')::uuid
  )
  from findings f where f.id = $1
`

/** Approve, and record the decision, exactly as core-api does. */
async function approve(finding: string) {
  const r = await app.query(APPROVE, [finding])
  if (r.rowCount === 0) return // already approved: Go writes nothing either
  await app.query(RECORD_DECISION, [finding, 'approve_finding'])
}

/** Reject, and record the decision, exactly as core-api does. */
async function reject(finding: string, reason: string) {
  const r = await app.query(
    `update findings
        set status = 'rejected', rejection_reason = nullif(btrim($2), ''),
            snoozed_until = null
      where id = $1
        and org_id = current_setting('app.current_org_id')::uuid
        and status <> 'rejected'`,
    [finding, reason],
  )
  if (r.rowCount === 0) return
  await app.query(RECORD_DECISION, [finding, 'reject_finding'])
}

const org = randomUUID()
const ada = randomUUID()

// Two obligations differing only in action_type, which is the variable under
// test. Everything else about them is identical on purpose.
const ropaObligation = randomUUID()
const reviewObligation = randomUUID()
const ropaSlug = `exec-ropa-${randomUUID().slice(0, 8)}`
const reviewSlug = `exec-review-${randomUUID().slice(0, 8)}`

let migrator: Client
let app: Client
let profile: string

const SUMMARY =
  'A fixture obligation standing in for a real one, long enough to satisfy the ' +
  'hundred character floor the schema places on an obligation summary.'

/**
 * Raises a signal and converts it the way the Analyst does, returning the
 * finding id.
 *
 * Run as the migrator rather than the app role, and that is not laziness:
 * `findings` has a select policy and an update policy and deliberately no
 * insert policy, so `kindlast_app` cannot create findings at all. 00002's
 * header says the producer functions run on a maintenance connection, never as
 * the application. Calling this as `app` would fail, and it should.
 */
async function convertSignal(slug: string): Promise<string> {
  const signal = randomUUID()
  await migrator.query(
    `insert into watcher_findings (id, org_id, profile_id, kind, obligation_slug, title, dedup_key)
     values ($1, $2, $3, 'profile_gap', $4, 'Fixture signal', $5)`,
    [signal, org, profile, slug, `dedup-${signal}`],
  )
  const r = await migrator.query(
    `select analyst_convert_signal($1) as finding_id`,
    [signal],
  )
  return r.rows[0].finding_id
}

async function auditActions(finding: string): Promise<string[]> {
  const r = await migrator.query(
    `select action_type from audit_log where finding_id = $1 order by occurred_at asc`,
    [finding],
  )
  return r.rows.map((row) => row.action_type)
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `Executor Fixture ${org.slice(0, 8)}`],
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

  await migrator.query(
    `insert into obligations
       (id, slug, title, summary, citation_celex, citation_kind, citation_article, action_type)
     values
       ($1, $2, 'Records of processing activities', $5, '32016R0679', 'article', 30, 'create_ropa'),
       ($3, $4, 'An obligation nobody has classified', $5, '32016R0679', 'article', 31, 'review')`,
    [ropaObligation, ropaSlug, reviewObligation, reviewSlug, SUMMARY],
  )
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id = $1`, [org])
  await migrator.query(`delete from obligations where id in ($1, $2)`, [
    ropaObligation,
    reviewObligation,
  ])
  await Promise.all([migrator.end(), app.end()])
})

describe.skipIf(!reachable)(
  'the obligation decides what approving does',
  () => {
    it('carries the obligation action onto the finding it creates', async () => {
      const finding = await convertSignal(ropaSlug)
      const r = await migrator.query(
        `select action_type from findings where id = $1`,
        [finding],
      )
      expect(r.rows[0].action_type).toBe('create_ropa')
    })

    it('defaults to review for an obligation nobody has classified', async () => {
      const finding = await convertSignal(reviewSlug)
      const r = await migrator.query(
        `select action_type from findings where id = $1`,
        [finding],
      )
      expect(r.rows[0].action_type).toBe('review')
    })
  },
)

describe.skipIf(!reachable)('an obligation that creates a record', () => {
  it('creates the processing activity on approval', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await approve(finding)

    const r = await migrator.query(
      `select id, org_id, created_by, name from processing_activities where finding_id = $1`,
      [finding],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].org_id).toBe(org)
    // The approver is recorded as the author, not the Analyst that raised it.
    expect(r.rows[0].created_by).toBe(ada)
  })

  // The one that closes 00006's loop. Before 00007 no trigger fired, so no row
  // carried a target_id, so this was null on every call.
  it('returns the id of the record it created', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await approve(finding)

    // What `approve_finding` used to return, read the way the Go store now
    // reads it (ENT-225): the audit row the Executor trigger wrote, found by
    // excluding rows whose target is the finding itself.
    //
    // Not by recency. Both audit rows are written in the same transaction and
    // `occurred_at` defaults to `now()`, the transaction timestamp, so they
    // carry an identical value and ordering by it decides nothing. The filter
    // is what makes this unambiguous.
    const r = await app.query(
      `select target_id from audit_log
        where finding_id = $1 and target_id is not null and target_table <> 'findings'`,
      [finding],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].target_id).not.toBeNull()

    const pa = await migrator.query(
      `select id from processing_activities where finding_id = $1`,
      [finding],
    )
    expect(r.rows[0].target_id).toBe(pa.rows[0].id)
  })

  // Two rows is the correct reading of two facts, not a duplicate: a human
  // decided, and a record was created. 00006's header says so and this pins it.
  //
  // Asserted as a set rather than a sequence, deliberately. In practice the
  // creation row lands first, because an AFTER UPDATE ... FOR EACH ROW trigger
  // completes before control returns to the function body, and 00006 relies on
  // exactly that to read the executor's target_id before writing its own row.
  // But that is trigger timing inside one transaction, not a promise this
  // schema makes, and a test asserting the order would fail the day someone
  // adds a BEFORE trigger without anything actually being wrong.
  //
  // The same rule applies to anything reading these rows: select the decision
  // by action_type, never by recency.
  it('writes two audit rows: the decision and the creation', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await approve(finding)

    const actions = await auditActions(finding)
    expect(actions).toHaveLength(2)
    expect(actions).toContain('approve_finding')
    expect(actions).toContain('create_ropa')
  })

  it('does not create a second record when approved again', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await approve(finding)
    await approve(finding)

    const r = await migrator.query(
      `select count(*)::int as n from processing_activities where finding_id = $1`,
      [finding],
    )
    expect(r.rows[0].n).toBe(1)
    expect(await auditActions(finding)).toHaveLength(2)
  })

  it('creates nothing when the finding is rejected instead', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await reject(finding, 'Not us')

    const r = await migrator.query(
      `select count(*)::int as n from processing_activities where finding_id = $1`,
      [finding],
    )
    expect(r.rows[0].n).toBe(0)
    expect(await auditActions(finding)).toEqual(['reject_finding'])
  })
})

describe.skipIf(!reachable)(
  'an unclassified obligation still creates nothing',
  () => {
    it('approving a review finding writes the decision and no record', async () => {
      const finding = await convertSignal(reviewSlug)
      await setTenant(app, org, ada)

      await approve(finding)

      // No creation row, and correctly so: a `review` obligation has no record
      // to navigate to. This is what the Go store reads to decide there is
      // nothing to send the person to.
      const created = await migrator.query(
        `select target_id from audit_log
          where finding_id = $1 and target_id is not null and target_table <> 'findings'`,
        [finding],
      )
      expect(created.rows).toHaveLength(0)
      expect(await auditActions(finding)).toEqual(['approve_finding'])

      const pa = await migrator.query(
        `select count(*)::int as n from processing_activities where finding_id = $1`,
        [finding],
      )
      expect(pa.rows[0].n).toBe(0)
    })
  },
)
