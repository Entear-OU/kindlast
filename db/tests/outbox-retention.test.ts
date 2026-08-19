/**
 * Retention on the transactional outbox (ENT-242, migration 00030).
 *
 * WHAT THIS SUITE IS ACTUALLY GUARDING
 *
 * `transactional_outbox.body_text` is, by construction, a store of raw
 * invitation tokens in plaintext: `notify.InvitationLink` puts the token in a
 * path segment and `notify.Invitation` renders that link into the body, because
 * the only egress a minted secret gets is at mint. Two tables away, 00003
 * stores only the token's hash, and says why: an invitation token is a bearer
 * credential and a database dump must not yield a working one.
 *
 * The outbox is the one place that rule is suspended, and it was suspended for
 * a bounded period: until the dispatcher drains the row. Nothing drained it, so
 * the bound did not exist. These tests are the bound.
 *
 * Four properties, and the order they are written in is the order they matter:
 *
 *   1. A pending message is NEVER touched while its invitation can still be
 *      accepted, whatever window the caller passes. This is the one whose
 *      failure loses a message silently and unrecoverably, because the raw
 *      token exists nowhere else and reissue is the only cure.
 *   2. Once the invitation can no longer be accepted, the body is gone, whether
 *      the message was delivered or not. Expired or accepted, the link is
 *      spent, and what is left is a dead credential and somebody's address.
 *   3. The delivery fact outlives the message text, and keeps outliving it. The
 *      reclaim deletes nothing.
 *   4. The removal path is the definer function and nothing else. No role holds
 *      a delete grant on this table, and `kindlast_app` no longer holds the
 *      update it was never supposed to have.
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

// Matches agent-role.test.ts and transactional-outbox.test.ts rather than
// importing from helpers/db, because the agent DSN is not part of the shared
// helper surface there either.
const AGENT_URL =
  process.env.PG_AGENT_URL ??
  'postgres://kindlast_agent:agent-dev-password@127.0.0.1:5433/kindlast'

const org = randomUUID()
const ada = randomUUID()

// Unique per run, so a suite that fails halfway and leaves rows behind cannot
// make the next run pass or fail for the wrong reason.
const run = randomUUID().slice(0, 8)

const LIVE = `live-${run}@example.invalid`
const EXPIRED = `expired-${run}@example.invalid`
const ACCEPTED = `accepted-${run}@example.invalid`
const DELIVERED_LIVE = `delivered-live-${run}@example.invalid`
const DELIVERED_SPENT = `delivered-spent-${run}@example.invalid`

// The thing that must not survive. Written into the body of every fixture
// message, exactly where `notify.Invitation` renders a real one.
const SECRET = `tok_${randomUUID().replaceAll('-', '')}`

let migrator: Client
let app: Client
let agent: Client

/** The reclaim, as the role that runs it in production. */
async function reclaim(bodyRetention: string, batch = 500) {
  const r = await agent.query(
    `select redacted, abandoned
       from reclaim_transactional_outbox($1::interval, $2)`,
    [bodyRetention, batch],
  )
  return {
    redacted: Number(r.rows[0].redacted),
    abandoned: Number(r.rows[0].abandoned),
  }
}

/** Read a fixture row back by the subject it was seeded with, which survives
 *  redaction only in the test's own bookkeeping: the column is cleared, so the
 *  lookup is by id captured at seed time. */
const ids = new Map<string, string>()

async function row(name: string) {
  await setTenant(migrator, org, ada)
  const r = await migrator.query(
    `select id, kind, status, recipient_email, subject, body_text, body_html,
            last_error, redacted_at, sent_at, created_at, attempts
       from transactional_outbox where id = $1`,
    [ids.get(name)],
  )
  return r.rows[0]
}

async function rowCount() {
  await setTenant(migrator, org, ada)
  const r = await migrator.query(
    `select count(*)::int as n from transactional_outbox where org_id = $1`,
    [org],
  )
  return r.rows[0].n as number
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `Outbox Retention ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )

  // Seeded through the migrator with a tenant set, because FORCE ROW LEVEL
  // SECURITY applies to the table owner too: 00014's owner-only insert policy is
  // evaluated for this connection exactly as it is for the application.
  await setTenant(migrator, org, ada)

  // An invitation for each message, in the state that message's fate turns on.
  // The pairing is the subject of this suite: the reclaim decides by asking
  // whether the invitation a message carries can still be accepted, and that
  // question is the only thing standing between a live invitation and a queue
  // that quietly empties itself.
  await migrator.query(
    `insert into invitations (org_id, email, role, token_hash, expires_at, accepted_at)
     values ($1, $2, 'member', $7,  now() + interval '7 days', null),
            ($1, $3, 'member', $8,  now() - interval '1 day',  null),
            ($1, $4, 'member', $9,  now() + interval '7 days', now()),
            ($1, $5, 'member', $10, now() + interval '7 days', null),
            ($1, $6, 'member', $11, now() + interval '7 days', now())`,
    [
      org,
      LIVE,
      EXPIRED,
      ACCEPTED,
      DELIVERED_LIVE,
      DELIVERED_SPENT,
      `hash-live-${run}`,
      `hash-expired-${run}`,
      `hash-accepted-${run}`,
      `hash-delivered-live-${run}`,
      `hash-delivered-spent-${run}`,
    ],
  )

  const body = `You have been invited. Accept at https://example.test/i/${SECRET}`

  // Three undelivered, one per invitation state.
  const undelivered = await migrator.query(
    `insert into transactional_outbox
       (org_id, kind, recipient_email, subject, body_text, created_at)
     values ($1, 'invitation', $2, 'live',     $5, now() - interval '1 hour'),
            ($1, 'invitation', $3, 'expired',  $5, now() - interval '8 days'),
            ($1, 'invitation', $4, 'accepted', $5, now() - interval '1 hour')
     returning id, subject`,
    [org, LIVE, EXPIRED, ACCEPTED, body],
  )

  // Two delivered. The first is thirty-one days old with an invitation that is
  // somehow still live, which is deliberately artificial: it is the only way to
  // exercise the window on its own, because in reality the invitation dies
  // first and that is exactly what the second disjunct is for. The second was
  // delivered an hour ago and has already been accepted.
  const delivered = await migrator.query(
    `insert into transactional_outbox
       (org_id, kind, recipient_email, subject, body_text, body_html,
        status, attempts, sent_at, created_at)
     values ($1, 'invitation', $2, 'delivered-live', $4, '<p>html</p>',
             'sent', 2, now() - interval '31 days', now() - interval '31 days'),
            ($1, 'invitation', $3, 'delivered-spent', $4, '<p>html</p>',
             'sent', 1, now() - interval '1 hour', now() - interval '1 hour')
     returning id, subject`,
    [org, DELIVERED_LIVE, DELIVERED_SPENT, body],
  )

  for (const r of [...undelivered.rows, ...delivered.rows]) {
    ids.set(r.subject, r.id)
  }
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from transactional_outbox where org_id = $1`, [
    org,
  ])
  await migrator.query(`delete from invitations where org_id = $1`, [org])
  await migrator.query(`delete from memberships where org_id = $1`, [org])
  await migrator.query(`delete from organisations where id = $1`, [org])
  await migrator.end()
  await app.end()
  await agent.end()
})

describe.skipIf(!reachable)('a delivered message', () => {
  it('keeps its body inside the body window', async () => {
    // Thirty-one days old, asked about with a window of sixty. The diagnostic
    // value of a delivered body is real and short: it answers "what did we
    // actually send this person", which is a question asked within days of a
    // complaint about a link that did not work.
    await reclaim('60 days')

    const r = await row('delivered-live')
    expect(r.body_text).toContain(SECRET)
    expect(r.redacted_at).toBeNull()
  })

  it('loses its body as soon as its link is spent, whatever the window', async () => {
    // The disjunct that makes the guarantee absolute rather than approximate.
    // This message was delivered an hour ago and the invitation has already
    // been accepted, so the token in the body is spent. A sixty-day window does
    // not save it, because there is no case in which keeping a spent credential
    // is worth holding somebody's address for two months.
    await reclaim('60 days')

    const r = await row('delivered-spent')
    expect(r, 'the row was deleted rather than redacted').toBeDefined()
    expect(r.redacted_at).not.toBeNull()
    expect(r.body_text).toBe('')
    expect(r.body_text).not.toContain(SECRET)
    expect(r.recipient_email).toBe('')
    expect(r.subject).toBe('')
    expect(r.body_html).toBeNull()
    // The postmark survives the envelope. This organisation sent an invitation,
    // at this time, and it went out on the first attempt.
    expect(r.status).toBe('sent')
    expect(r.sent_at).not.toBeNull()
    expect(r.attempts).toBe(1)
  })

  it('loses its body past the body window', async () => {
    const result = await reclaim('7 days')
    expect(result.redacted, 'the aged delivered row was not redacted').toBe(1)

    const r = await row('delivered-live')
    expect(r.redacted_at).not.toBeNull()
    expect(r.body_text).toBe('')
    expect(r.status).toBe('sent')
    expect(r.attempts).toBe(2)
  })

  it('is not redacted twice', async () => {
    // Idempotence, which is what makes the job safe to run every hour and safe
    // to run in more than one replica at once.
    const result = await reclaim('7 days')
    expect(result.redacted).toBe(0)
  })
})

describe.skipIf(!reachable)('a message that can no longer be delivered', () => {
  it('is abandoned once its invitation has expired', async () => {
    await reclaim('7 days')

    const r = await row('expired')
    expect(
      r,
      'an undelivered message was deleted rather than abandoned',
    ).toBeDefined()
    // `failed` is 00014's word for giving up, and this is the first thing in
    // the codebase to write it. It matters beyond bookkeeping: the dispatcher
    // claims `status = 'pending'` and has no maximum attempt count, so without
    // this it retries a permanently undeliverable message every ten seconds
    // for as long as the deployment lives.
    expect(r.status).toBe('failed')
    expect(r.redacted_at).not.toBeNull()
    expect(r.body_text).toBe('')
    expect(r.recipient_email).toBe('')
    expect(r.last_error).toContain('abandoned undelivered')
  })

  it('is abandoned once its invitation has been accepted', async () => {
    await reclaim('7 days')

    const r = await row('accepted')
    expect(r.status).toBe('failed')
    expect(r.redacted_at).not.toBeNull()
    expect(r.body_text).toBe('')
  })

  it('is no longer claimable by the dispatcher', async () => {
    // Read with no tenant GUC at all, as the dispatcher runs.
    const r = await agent.query(
      `select recipient_email from transactional_outbox
        where status = 'pending' and recipient_email in ($1, $2, $3)`,
      [LIVE, EXPIRED, ACCEPTED],
    )
    const seen = r.rows.map((x) => x.recipient_email)
    expect(seen).toEqual([LIVE])
  })

  it('leaves no raw token anywhere in the table for a dead invitation', async () => {
    // The property 00003 and 00014 both assert and neither delivered: a
    // database dump must not yield a working invitation. Asked of the whole
    // table rather than of a row, because "we redacted the rows we thought of"
    // is not the claim being made.
    const r = await agent.query(
      `select count(*)::int as n from transactional_outbox
        where body_text like $1 and status <> 'pending'`,
      [`%${SECRET}%`],
    )
    expect(r.rows[0].n, 'a spent or expired token is still in the clear').toBe(
      0,
    )
  })
})

describe.skipIf(!reachable)('the reclaim never removes anything', () => {
  it('leaves every row it has processed in place', async () => {
    // The shape of this fix. The row is two separable things: a delivery fact,
    // and a rendered message holding a credential and an address. Deleting it
    // would drop the data by throwing away the fact. Only the cascade from
    // `organisations` removes a row from this table, which is how erasing an
    // organisation already works.
    await reclaim('7 days')
    expect(await rowCount()).toBe(5)
  })
})

describe.skipIf(!reachable)('the redaction invariant', () => {
  it('refuses a redaction recorded on a message still waiting to be sent', async () => {
    // A constraint rather than a predicate inside the function, because it must
    // hold no matter who writes. `kindlast_agent` holds an unconditional update
    // policy on this table, so without this any code path in the dispatcher
    // could blank a pending message and stamp it, destroying a token that
    // exists nowhere else.
    await setTenant(migrator, org, ada)
    const raised = await refused(
      migrator,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text, redacted_at)
       values ($1, 'invitation', '', '', '', now())`,
      [org],
    )
    expect(raised, 'a pending row was recorded as redacted').not.toBeNull()
    expect(raised?.code).toBe('23514')
  })

  it('refuses a redaction that left the body behind', async () => {
    // The shape of lie nobody notices: the row says the personal data is gone
    // and the column still holds it.
    await setTenant(migrator, org, ada)
    const raised = await refused(
      migrator,
      `insert into transactional_outbox
         (org_id, kind, recipient_email, subject, body_text,
          status, sent_at, redacted_at)
       values ($1, 'invitation', '', '', 'the token is still here',
               'sent', now(), now())`,
      [org],
    )
    expect(
      raised,
      'a row claimed redaction with its body intact',
    ).not.toBeNull()
    expect(raised?.code).toBe('23514')
  })
})

describe.skipIf(!reachable)('who may reclaim', () => {
  it('holds no delete grant for either role, and no update for the application', async () => {
    // 00014's header says the application enqueues and reads, and the grant did
    // not say so: 00002's default privileges attached full DML to every table
    // created after it, so this one arrived with update and delete already on
    // it, and only the absent policies were holding. 00015 already decided that
    // for a table of bearer credentials a missing grant, which fails loudly at
    // parse time, is worth more than a missing policy, which fails quietly at
    // run time.
    const r = await migrator.query(
      `select grantee, privilege_type
         from information_schema.role_table_grants
        where table_name = 'transactional_outbox'
          and grantee in ('kindlast_app', 'kindlast_agent')
        order by grantee, privilege_type`,
    )
    const held = r.rows.map((g) => `${g.grantee}:${g.privilege_type}`)
    expect(held).toEqual([
      'kindlast_agent:SELECT',
      'kindlast_agent:UPDATE',
      'kindlast_app:INSERT',
      'kindlast_app:SELECT',
    ])
  })

  it('refuses the application the reclaim', async () => {
    // The removal path is the function and nothing else, so the execute grant
    // is the whole of it. The application never reclaims: it serves requests,
    // and a request handler that could reclaim could blank a queued invitation
    // somebody is waiting for.
    const raised = await refused(
      app,
      `select reclaim_transactional_outbox('7 days'::interval, 10)`,
    )
    expect(raised, 'the application executed the reclaim').not.toBeNull()
    expect(raised?.code).toBe('42501')
  })

  it('refuses the dispatcher a direct read of who has been invited', async () => {
    // Why the function is `security definer` at all. Deciding an undelivered
    // message's fate means asking whether its invitation can still be accepted,
    // and that is a read of `invitations`, where the agent has no grant by
    // design (00008). Granting one would hand a compromised agent every invited
    // address in every tenant, which is the same widening 00015 refused for
    // `notification_recipients`.
    const raised = await refused(agent, `select email from invitations limit 1`)
    expect(raised, 'the dispatcher read the invitations table').not.toBeNull()
    expect(raised?.code).toBe('42501')
  })
})

describe.skipIf(!reachable)('a message that can still be delivered', () => {
  it('survives a reclaim with the body window at zero', async () => {
    // THE TEST THIS SUITE EXISTS FOR, and the one whose failure is silent.
    //
    // The window is zero, which is the most aggressive request a caller can
    // make, and this message must still be here with its token intact. What
    // saves it is not a window at all: it is that an invitation which can still
    // be accepted has a message that can still be usefully sent, and the raw
    // token in that body exists nowhere else in the system. 00003 keeps only
    // the hash. Blanking the body is not a retention decision, it is destroying
    // the only copy of a credential the recipient is waiting for, and nobody
    // would know which invitations needed reissuing.
    //
    // Zero rather than a short interval on purpose. A caller passing a window
    // that is too short is the mistake this has to survive, so the test asks
    // for the worst one there is.
    //
    // Last in the file deliberately: it is the only call that would redact the
    // aged delivered fixture early, and the tests above measure that window.
    const before = await row('live')
    await reclaim('0')
    const after = await row('live')

    expect(
      after,
      'a message whose invitation is still live was deleted',
    ).toBeDefined()
    expect(after.status, 'a deliverable message was abandoned').toBe('pending')
    expect(
      after.body_text,
      'the token was stripped from a message that has not been sent',
    ).toContain(SECRET)
    expect(after.recipient_email).toBe(LIVE)
    expect(
      after.redacted_at,
      'a pending message was recorded as redacted',
    ).toBeNull()
    expect(after.body_text).toBe(before.body_text)
  })

  it('is still the only row the dispatcher can claim', async () => {
    await reclaim('0')
    const r = await agent.query(
      `select recipient_email from transactional_outbox
        where status = 'pending' and org_id = $1`,
      [org],
    )
    expect(r.rows.map((x) => x.recipient_email)).toEqual([LIVE])
  })
})

/**
 * Run a statement that must be refused, and return the error it raised, or null
 * if it was allowed through.
 *
 * Wrapped in its own transaction because a failure aborts the transaction it
 * happens in (SQLSTATE 25P02), so without a boundary the first expected failure
 * poisons every assertion after it. The session GUCs survive: setTenant writes
 * them with `set_config(..., false)`, which is session-wide rather than
 * transaction-local.
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
