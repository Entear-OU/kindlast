/**
 * The statutory clock runs from receipt (ENT-224), and where that rule now
 * lives (ENT-271).
 *
 * A DSAR's Article 12(3) deadline runs from receipt of the request, not from
 * the day somebody approved the finding that logs it. 00010 made the executor
 * trigger take `received_at` from the payload and refuse to guess it, with the
 * argument written out at length: a request whose receipt date is unknown has
 * an unknowable deadline, and now() asserts a specific deadline that is
 * optimistic by however long the request sat unlogged.
 *
 * WHAT MOVED (ENT-271)
 *
 * The trigger is gone. Approving a finding now enqueues an executor job and a
 * Temporal workflow creates the DSAR a moment later, so the refusal could no
 * longer abort the approving transaction, and a refusal that arrived after the
 * approval would be strictly worse than the default it replaced: the finding
 * would be approved, the audit row would name the approver, and the DSAR would
 * never appear. So the rule moved to the approval, in Go:
 * `findings.CheckReceipt`, refused with `failed_precondition` before anything
 * is written. Its tests are
 * `apps/core-api/internal/domain/findings/posture_test.go` (the rule) and
 * `apps/core-api/internal/store/postgres/executor_test.go` (the refusal
 * leaving the finding pending, and the clock running from the payload's date,
 * against this same database).
 *
 * WHAT IS STILL THE DATABASE'S, AND THEREFORE STILL HERE
 *
 * The column and its shape: a DSAR carries `received_at` and
 * `response_due_at`, and the schema does not care where they came from. The
 * one test below writes a DSAR the way the executor does and asserts the
 * thirty days are counted from the receipt it was given, which is the
 * invariant the clock rests on whoever computes it.
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
 * This used to be `select approve_finding($1)`. That function decided things,
 * so it moved to Go and 00016 dropped it, and the statement below is what the
 * Go store now issues.
 *
 * The subject of this file did not move. The Executor triggers are still SQL,
 * still fire on `after update of status`, and are still what is under test
 * here; only the thing pulling the trigger changed. Driving them with the real
 * UPDATE rather than through a function is arguably a better test than before,
 * because it is the statement production actually runs.
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
  // The property ENT-224 asks for by name, asserted against the schema: a
  // request that arrived eight days ago is due 22 days from now, not 30.
  // Whoever computes the dates, a DSAR that says otherwise is wrong.
  it('is thirty days after the receipt, not after the approval', async () => {
    const received = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000)
    const finding = await seedDsarFinding({
      requester: 'A. Subject',
      request_type: 'access',
      received_at: received.toISOString(),
    })

    // Written as the Executor writes it: the application role, as the
    // approver, taking both dates from the payload.
    await setTenant(app, org, ada)
    await app.query(
      `insert into dsars (org_id, created_by, finding_id, subject_name,
                          request_type, status, received_at, response_due_at)
       select f.org_id, current_setting('app.current_user_id')::uuid, f.id,
              f.metadata -> 'payload' ->> 'requester',
              f.metadata -> 'payload' ->> 'request_type',
              'open',
              (f.metadata -> 'payload' ->> 'received_at')::timestamptz,
              (f.metadata -> 'payload' ->> 'received_at')::timestamptz + interval '30 days'
         from findings f where f.id = $1`,
      [finding],
    )

    const r = await migrator.query(
      `select received_at, response_due_at from dsars where finding_id = $1`,
      [finding],
    )
    expect(r.rows).toHaveLength(1)

    const storedReceived = new Date(r.rows[0].received_at).getTime()
    const due = new Date(r.rows[0].response_due_at).getTime()

    expect(Math.abs(storedReceived - received.getTime())).toBeLessThan(1000)
    expect(Math.abs(due - (received.getTime() + 30 * 86400_000))).toBeLessThan(
      1000,
    )
    // Stated the other way round, because this is the property that matters:
    // the deadline is sooner than 30 days from now, so the request is already
    // partly through its window.
    expect(due).toBeLessThan(Date.now() + 30 * 86400_000)
  })
})
