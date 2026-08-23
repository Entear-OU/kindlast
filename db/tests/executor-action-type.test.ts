/**
 * What the obligation decides, and what the approval leaves behind (ENT-165,
 * ENT-203, rewritten by ENT-271).
 *
 * `findings.action_type` was read by three triggers and written by nothing:
 * `analyst_convert_signal`'s INSERT column list omitted it, so every finding
 * got the column default `'review'` and none of the three executor triggers
 * could ever fire. 00007 puts the action on the obligation, where the
 * knowledge is: what approving should *do* is a property of the regulatory
 * requirement, not of whichever sweep happened to notice it.
 *
 * WHAT THIS FILE STOPPED TESTING, AND WHERE IT WENT (ENT-271)
 *
 * The three executor triggers are gone (00036). Approving no longer creates
 * the record inside the transaction; it writes an `executor_jobs` row, and a
 * Temporal workflow creates the record a moment later, as the approver. So
 * the assertions about the record and its audit row moved to Go, where the
 * code that creates them now lives:
 * `apps/core-api/internal/store/postgres/executor_test.go`, against this same
 * database.
 *
 * What is still the database's, and therefore still here: the obligation
 * decides the action type, an approval of a record-creating finding leaves a
 * job for the Executor, an approval of a `review` finding leaves none, and a
 * rejection leaves neither a job nor a record.
 *
 * Proven able to fail: dropping `action_type` from 00007's INSERT column list
 * (i.e. restoring the ENT-165 bug) turns the action-type tests red and leaves
 * the `review` ones green, which is the right shape.
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
/** The executor job the approval enqueues (00036), as the Go store writes it. */
const ENQUEUE_JOB = `
  insert into executor_jobs (org_id, finding_id, action_type, approved_by)
  select f.org_id, f.id, f.action_type, current_setting('app.current_user_id')::uuid
    from findings f
   where f.id = $1
     and f.action_type in ('create_ropa', 'create_dsar', 'create_ai_system')
  on conflict (finding_id) do nothing
`

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
  // And the job, for the three action types that create a record (ENT-271).
  // The Go store does exactly this, in the same transaction; this helper
  // exists to mirror it, so a test here is a test of what the database does
  // with what core-api writes.
  await app.query(ENQUEUE_JOB, [finding])
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
  it('leaves a job for the Executor and no record of its own', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await approve(finding)

    // THE CHANGE ENT-271 MADE, ASSERTED HERE RATHER THAN INFERRED. The
    // trigger used to have inserted the processing activity inside the
    // transaction that approved. Now the approval leaves a job, and the
    // record arrives when the workflow runs.
    const job = await migrator.query(
      `select org_id, action_type, approved_by, status
         from executor_jobs where finding_id = $1`,
      [finding],
    )
    expect(job.rows).toHaveLength(1)
    expect(job.rows[0].action_type).toBe('create_ropa')
    expect(job.rows[0].status).toBe('pending')
    expect(job.rows[0].org_id).toBe(org)
    // The approver, because the record will be created by their decision and
    // the execution runs as them.
    expect(job.rows[0].approved_by).toBe(ada)

    const pa = await migrator.query(
      `select count(*)::int as n from processing_activities where finding_id = $1`,
      [finding],
    )
    expect(pa.rows[0].n).toBe(0)

    // And the decision is in the audit log, alone: the creation row is
    // written by the execution, later.
    expect(await auditActions(finding)).toEqual(['approve_finding'])
  })

  it('leaves one job however many times it is approved', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await approve(finding)
    await approve(finding)

    const job = await migrator.query(
      `select count(*)::int as n from executor_jobs where finding_id = $1`,
      [finding],
    )
    expect(job.rows[0].n).toBe(1)
  })

  it('leaves nothing at all when the finding is rejected instead', async () => {
    const finding = await convertSignal(ropaSlug)
    await setTenant(app, org, ada)

    await reject(finding, 'we do not do this')

    const r = await migrator.query(
      `select (select count(*)::int from executor_jobs where finding_id = $1) as jobs,
              (select count(*)::int from processing_activities where finding_id = $1) as records`,
      [finding],
    )
    expect(r.rows[0].jobs).toBe(0)
    expect(r.rows[0].records).toBe(0)
  })
})

describe.skipIf(!reachable)(
  'an unclassified obligation still creates nothing',
  () => {
    it('approving a review finding writes the decision and no job', async () => {
      const finding = await convertSignal(reviewSlug)
      await setTenant(app, org, ada)

      await approve(finding)

      expect(await auditActions(finding)).toEqual(['approve_finding'])
      const r = await migrator.query(
        `select (select count(*)::int from executor_jobs where finding_id = $1) as jobs,
                (select count(*)::int from processing_activities where finding_id = $1) as records`,
        [finding],
      )
      expect(r.rows[0].jobs).toBe(0)
      expect(r.rows[0].records).toBe(0)
    })
  },
)
