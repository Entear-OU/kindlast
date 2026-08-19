/**
 * The transactional outbox and the two roles that touch it (ENT-219,
 * migration 00014).
 *
 * WHAT THIS SUITE IS ACTUALLY GUARDING
 *
 * The table holds an arbitrary recipient address and an arbitrary body, and a
 * background process delivers whatever is in it. That is a mail relay, and the
 * only thing standing between it and being an open one is the policy set. So
 * these tests are not "does the insert work": they are the boundary.
 *
 * Three properties, each of which fails silently if it regresses:
 *
 *   1. Only an owner can cause a message. A member or a viewer who could insert
 *      could send mail from this deployment's domain to any address they like.
 *   2. The application cannot mark a message delivered. `sent` is what a
 *      regulator would read as evidence the invitation went out, and a request
 *      handler that could write it could assert a delivery that never happened.
 *   3. The dispatcher reads every organisation, and that is deliberate rather
 *      than an accident of a missing predicate. It is asserted positively here
 *      so that nobody "fixes" it later by adding an org check and quietly
 *      stopping delivery for every organisation but one.
 *
 * The split-brain constraint is tested too, because it is the part of "a
 * delivered row is not re-sent" that a code review cannot forget to check.
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

// Matches agent-role.test.ts rather than importing from helpers/db, because the
// agent DSN is not part of the shared helper surface there either.
const AGENT_URL =
  process.env.PG_AGENT_URL ??
  'postgres://kindlast_agent:agent-dev-password@127.0.0.1:5433/kindlast'

// Two organisations. Ada owns the first, Miko is a plain member of it, and Bob
// owns the second and must never be visible to the other two.
const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()
const miko = randomUUID()
const bob = randomUUID()

// Fixture recipients, unique per run so a suite that fails halfway and leaves
// rows behind cannot make the next run pass or fail for the wrong reason.
const run = randomUUID().slice(0, 8)
const PENDING_A = `a-pending-${run}@example.invalid`
const DELIVERABLE_A = `a-deliverable-${run}@example.invalid`
const PENDING_B = `b-pending-${run}@example.invalid`

let migrator: Client
let app: Client
let agent: Client

/**
 * Run a statement that must be refused, and return the error it raised, or null
 * if it was allowed through.
 *
 * Wrapped in its own transaction for two reasons. A policy violation aborts the
 * transaction it happens in (SQLSTATE 25P02), so without a boundary the first
 * expected failure poisons every assertion after it and the suite reports a
 * cascade rather than one result. And a statement that is wrongly *allowed*
 * gets rolled back rather than leaving a row behind that later assertions would
 * then count.
 *
 * The session GUCs survive: setTenant writes them with `set_config(..., false)`,
 * which is session-wide rather than transaction-local, so the tenant a test set
 * still applies inside here.
 */
async function refused(c: Client, sql: string, params: unknown[] = []) {
  await c.query('begin')
  try {
    await c.query(sql, params)
    return null
  } catch (error) {
    return error as Error & { code?: string }
  } finally {
    await c.query('rollback')
  }
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)

  for (const [org, name] of [
    [orgA, 'Outbox Fixture A'],
    [orgB, 'Outbox Fixture B'],
  ] as const) {
    await migrator.query(
      `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
      [org, `${name} ${org.slice(0, 8)}`],
    )
  }

  await migrator.query(
    `insert into memberships (org_id, user_id, role) values
       ($1, $2, 'owner'), ($1, $3, 'member'), ($4, $5, 'owner')`,
    [orgA, ada, miko, orgB, bob],
  )

  // Fixture rows, seeded here rather than left as a side effect of an earlier
  // test. Every row a test reads is one this block created, so the suite does
  // not depend on execution order and a single test can be run alone with -t.
  //
  // Seeded through the migrator with a tenant set, because FORCE ROW LEVEL
  // SECURITY applies to the table owner too: the owner-only insert policy is
  // evaluated for this connection exactly as it is for the application.
  await setTenant(migrator, orgA, ada)
  await migrator.query(
    `insert into transactional_outbox
       (org_id, kind, recipient_email, subject, body_text)
     values ($1, 'invitation', $2, 'A pending', 'body'),
            ($1, 'invitation', $3, 'A deliverable', 'body')`,
    [orgA, PENDING_A, DELIVERABLE_A],
  )

  await setTenant(migrator, orgB, bob)
  await migrator.query(
    `insert into transactional_outbox
       (org_id, kind, recipient_email, subject, body_text)
     values ($1, 'invitation', $2, 'B pending', 'body')`,
    [orgB, PENDING_B],
  )
})

/** Look a fixture row up as the superuser-equivalent migrator, bypassing no RLS
 *  but with a tenant that can see it, so a test can assert on the stored row
 *  rather than on what the actor under test was allowed to read back. */
async function rowByEmail(email: string, org: string, user: string) {
  await setTenant(migrator, org, user)
  const r = await migrator.query(
    `select id, status, sent_at, attempts from transactional_outbox where recipient_email = $1`,
    [email],
  )
  return r.rows[0]
}

afterAll(async () => {
  if (!reachable) return
  await migrator.query(
    `delete from transactional_outbox where org_id = any($1)`,
    [[orgA, orgB]],
  )
  await migrator.query(`delete from memberships where org_id = any($1)`, [
    [orgA, orgB],
  ])
  await migrator.query(`delete from organisations where id = any($1)`, [
    [orgA, orgB],
  ])
  await migrator.end()
  await app.end()
  await agent.end()
})

describe.skipIf(!reachable)('transactional outbox', () => {
  it('an owner can enqueue a message for their own organisation', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text)
       values ($1, 'invitation', $2, 'You are invited', 'link')
       returning id, status, attempts, sent_at`,
      [orgA, `enqueued-${run}@example.invalid`],
    )
    expect(r.rows).toHaveLength(1)
    // A freshly enqueued row is pending, unattempted, and not sent. The
    // dispatcher's claim query depends on all three.
    expect(r.rows[0].status).toBe('pending')
    expect(r.rows[0].attempts).toBe(0)
    expect(r.rows[0].sent_at).toBeNull()
  })

  it('a member who is not an owner cannot enqueue anything', async () => {
    // The mail-relay boundary. Miko is a genuine member of orgA, so this is not
    // a tenancy question: it is whether ordinary membership is enough to make
    // the deployment send mail to an address of your choosing.
    await setTenant(app, orgA, miko)
    const error = await refused(
      app,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text)
       values ($1, 'invitation', 'attacker@example.invalid', 'hello', 'body')`,
      [orgA],
    )
    expect(
      error,
      'a plain member was allowed to enqueue a message',
    ).not.toBeNull()
    expect(error?.code).toBe('42501')
  })

  it('an owner cannot enqueue into an organisation they do not belong to', async () => {
    await setTenant(app, orgA, ada)
    const error = await refused(
      app,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text)
       values ($1, 'invitation', 'invitee@example.invalid', 'hello', 'body')`,
      [orgB],
    )
    expect(
      error,
      'an owner wrote a row into another organisation',
    ).not.toBeNull()
  })

  it('an owner can read their own organisation queued messages', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `select recipient_email from transactional_outbox where org_id = $1`,
      [orgA],
    )
    const seen = r.rows.map((row) => row.recipient_email)
    expect(seen).toContain(PENDING_A)
  })

  it('an organisation cannot read another organisation queued messages', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `select id from transactional_outbox where recipient_email = $1`,
      [PENDING_B],
    )
    expect(r.rows).toHaveLength(0)
  })

  it('the application cannot mark a message delivered, and is told so', async () => {
    // There is no update policy for kindlast_app, deliberately. `sent` is what
    // a regulator reads as evidence the invitation went out, and the thing that
    // serves requests must not be able to assert a delivery it did not perform.
    //
    // THIS TEST CHANGED SHAPE IN ENT-242, AND THE CHANGE IS THE POINT.
    //
    // It used to assert that the update touched zero rows, because that is what
    // a missing policy does: the statement succeeds having changed nothing. The
    // comment here used to explain that the silence was why the row had to be
    // checked afterwards. It was right about the mechanism and it was
    // describing a boundary that failed quietly, which for a table holding
    // bearer tokens is the wrong kind.
    //
    // 00030 revoked the update grant that 00002's default privileges had
    // attached and 00014's comment already claimed was absent, so the refusal
    // now arrives at parse time as 42501. Both halves are still asserted: the
    // error, and the row, because a test that only looked for an exception
    // would pass against a table that raised for some other reason.
    const before = await rowByEmail(PENDING_A, orgA, ada)

    await setTenant(app, orgA, ada)
    const error = await refused(
      app,
      `update transactional_outbox set status = 'sent', sent_at = now() where id = $1`,
      [before.id],
    )
    expect(error, 'the application updated a queued message').not.toBeNull()
    expect(error?.code).toBe('42501')

    const after = await rowByEmail(PENDING_A, orgA, ada)
    expect(
      after.status,
      'the message was marked delivered by the application',
    ).toBe('pending')
    expect(after.sent_at).toBeNull()
  })

  it('the application cannot delete a queued message, and is told so', async () => {
    // Same change, same reason. After 00030 nothing deletes from this table at
    // all: retention is redaction, and the only removal is the cascade from
    // `organisations`.
    const before = await rowByEmail(PENDING_A, orgA, ada)

    await setTenant(app, orgA, ada)
    const error = await refused(
      app,
      `delete from transactional_outbox where id = $1`,
      [before.id],
    )
    expect(error, 'the application deleted a queued message').not.toBeNull()
    expect(error?.code).toBe('42501')

    const after = await rowByEmail(PENDING_A, orgA, ada)
    expect(after, 'the message is gone').toBeDefined()
  })
})

describe.skipIf(!reachable)('the dispatcher role', () => {
  it('reads pending rows across every organisation', async () => {
    // Asserted positively and deliberately. Draining an outbox is inherently
    // cross-tenant, and if someone later "hardens" this by adding an org
    // predicate, delivery silently stops for every organisation but one. This
    // test is what should fail then.
    // No tenant GUC is set on this connection at all, which is the point: the
    // dispatcher is a system process with no organisation of its own.
    const r = await agent.query(
      `select recipient_email from transactional_outbox where status = 'pending'`,
    )
    const seen = new Set(r.rows.map((row) => row.recipient_email))
    expect(
      seen.has(PENDING_A),
      'the dispatcher cannot see organisation A',
    ).toBe(true)
    expect(
      seen.has(PENDING_B),
      'the dispatcher cannot see organisation B',
    ).toBe(true)
  })

  it('can record the outcome of a delivery', async () => {
    const updated = await agent.query(
      `update transactional_outbox
          set status = 'sent', sent_at = now(), attempts = attempts + 1
        where recipient_email = $1
        returning status, sent_at, attempts`,
      [DELIVERABLE_A],
    )
    expect(updated.rowCount, 'the dispatcher could not update the row').toBe(1)
    expect(updated.rows[0].status).toBe('sent')
    expect(updated.rows[0].sent_at).not.toBeNull()
    expect(updated.rows[0].attempts).toBe(1)
  })

  it('does not see a row it has already delivered', async () => {
    // The acceptance criterion, at the level the dispatcher's claim query
    // actually runs: once marked sent, the row is no longer pending, so the
    // next drain cannot pick it up and deliver it a second time.
    const r = await agent.query(
      `select recipient_email from transactional_outbox where status = 'pending'`,
    )
    const seen = new Set(r.rows.map((row) => row.recipient_email))
    expect(
      seen.has(DELIVERABLE_A),
      'a delivered message is still pending',
    ).toBe(false)
  })

  it('cannot create a message of its own', async () => {
    // The compensating control for the unconditional read policy: the role that
    // delivers cannot author. Without this the dispatcher would be able to send
    // anything to anyone, which is precisely what the owner-only insert policy
    // exists to prevent on the other side.
    const error = await refused(
      agent,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text)
       values ($1, 'invitation', 'forged@example.invalid', 'forged', 'body')`,
      [orgA],
    )
    expect(error, 'the dispatcher forged a message').not.toBeNull()
    expect(error?.code).toBe('42501')
  })
})

describe.skipIf(!reachable)('the delivered-once constraint', () => {
  it('refuses sent with no timestamp', async () => {
    // Storable without the constraint, and a claim query filtering on
    // `sent_at is null` would hand this row straight back to the dispatcher.
    const error = await refused(
      migrator,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text, status)
       values ($1, 'invitation', 'x@example.invalid', 's', 'b', 'sent')`,
      [orgA],
    )
    expect(error, 'a sent row with no sent_at was accepted').not.toBeNull()
    expect(error?.code).toBe('23514')
  })

  it('refuses a timestamp without sent', async () => {
    const error = await refused(
      migrator,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text, status, sent_at)
       values ($1, 'invitation', 'y@example.invalid', 's', 'b', 'pending', now())`,
      [orgA],
    )
    expect(error, 'a pending row carrying sent_at was accepted').not.toBeNull()
    expect(error?.code).toBe('23514')
  })

  it('refuses an unknown kind', async () => {
    const error = await refused(
      migrator,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text)
       values ($1, 'password_reset', 'z@example.invalid', 's', 'b')`,
      [orgA],
    )
    expect(error, 'an unrecognised kind was accepted').not.toBeNull()
    expect(error?.code).toBe('23514')
  })
})
