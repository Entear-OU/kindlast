/**
 * Answering the doorbell, and saying stop ringing it (ENT-209, migration
 * 00015).
 *
 * WHAT IS ACTUALLY AT RISK HERE
 *
 * Three different things, and they fail in three different ways.
 *
 * The dispatch policies widen `kindlast_agent` on `notification_outbox` to
 * SELECT and UPDATE across every organisation. That is deliberate, because
 * draining an outbox is cross-tenant by nature, and it is exactly the kind of
 * widening somebody later "tightens" by adding an org predicate, which stops
 * delivery everywhere silently. What must NOT widen with it is INSERT: the
 * agent may only ring a doorbell for the organisation its GUC names.
 *
 * `notification_recipients` exists so the agent does not need table grants on
 * memberships, preferences and identities. If it ever returns more than the
 * people attached to the outbox row it was handed, that argument collapses and
 * the narrow-grant design was pointless.
 *
 * `redeem_capability_token` runs for somebody with no session at all. The token
 * is the only identity claim, so single use, expiry and an indistinguishable
 * answer for expired, used, wrong-kind and never-existed are the whole of its
 * security. Each is asserted separately, because a redemption path that is
 * merely usually-single-use is not single use.
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

const reachable = await isStackReachable()

const AGENT_URL = roleUrl('agent')

// Two organisations. Ada owns A and has an address; Miko is a member of A with
// no identity row at all, which is the state of somebody invited who has never
// signed in. Bob owns B and must never appear in A's recipients.
const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()
const miko = randomUUID()
const bob = randomUUID()

const obligationID = randomUUID()
const obligationSlug = `notif-dispatch-${randomUUID().slice(0, 8)}`

let migrator: Client
let app: Client
let agent: Client

let findingA = ''
let findingB = ''
let outboxA = ''
let outboxB = ''

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

/** A finding, with the whole chain it needs underneath it. The insert trips
 *  `enqueue_finding_notification`, so an outbox row appears as a side effect,
 *  which is the behaviour under test rather than a fixture shortcut. */
async function seedFinding(org: string, actor: string): Promise<string> {
  const session = randomUUID()
  const profile = randomUUID()
  const signal = randomUUID()
  const finding = randomUUID()

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
        detected, proposed_action, severity)
     values ($1, $2, $3, $4, $5, $6, 'Fixture gap', 'Fixture action', 'high')`,
    [finding, org, profile, signal, obligationID, obligationSlug],
  )
  return finding
}

async function outboxFor(finding: string): Promise<string> {
  const r = await migrator.query(
    `select id from notification_outbox where finding_id = $1`,
    [finding],
  )
  return r.rows[0]?.id ?? ''
}

/** Mint a capability token the way the dispatcher would. */
async function mintToken(
  org: string,
  user: string,
  hash: string,
  kind = 'unsubscribe',
  expiresIn = '7 days',
) {
  await agent.query(
    `insert into capability_tokens (org_id, kind, token_hash, user_id, expires_at)
     values ($1, $2, $3, $4, now() + $5::interval)`,
    [org, kind, hash, user, expiresIn],
  )
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)

  for (const [org, name] of [
    [orgA, 'Doorbell Fixture A'],
    [orgB, 'Doorbell Fixture B'],
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

  // Ada and Bob have signed in; Miko has not, so has no identity and therefore
  // no address. That is a real state, not a hypothetical one.
  for (const [user, handle] of [
    [ada, 'ada'],
    [bob, 'bob'],
  ] as const) {
    await migrator.query(
      `insert into user_identities (user_id, issuer, subject, email, display_name)
       values ($1, 'https://test.invalid', $2, $3, $4)`,
      [
        user,
        `subject-${user}`,
        `${handle}-${user.slice(0, 8)}@example.invalid`,
        handle,
      ],
    )
  }

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

  findingA = await seedFinding(orgA, ada)
  findingB = await seedFinding(orgB, bob)
  outboxA = await outboxFor(findingA)
  outboxB = await outboxFor(findingB)
})

afterAll(async () => {
  if (!reachable) return
  const orgs = [orgA, orgB]
  await migrator.query(`delete from capability_tokens where org_id = any($1)`, [
    orgs,
  ])
  await migrator.query(
    `delete from notification_outbox where org_id = any($1)`,
    [orgs],
  )
  await migrator.query(`delete from findings where org_id = any($1)`, [orgs])
  await migrator.query(`delete from watcher_findings where org_id = any($1)`, [
    orgs,
  ])
  await migrator.query(
    `delete from compliance_profiles where org_id = any($1)`,
    [orgs],
  )
  await migrator.query(
    `delete from onboarding_sessions where org_id = any($1)`,
    [orgs],
  )
  await migrator.query(
    `delete from notification_preferences where org_id = any($1)`,
    [orgs],
  )
  await migrator.query(
    `delete from notification_channels where org_id = any($1)`,
    [orgs],
  )
  await migrator.query(`delete from obligations where id = $1`, [obligationID])
  await migrator.query(`delete from memberships where org_id = any($1)`, [orgs])
  await migrator.query(`delete from user_identities where user_id = any($1)`, [
    [ada, bob],
  ])
  await migrator.query(`delete from organisations where id = any($1)`, [orgs])
  await migrator.end()
  await app.end()
  await agent.end()
})

describe.skipIf(!reachable)('the doorbell rings by itself', () => {
  it('inserting a finding enqueues exactly one outbox row', () => {
    // The trigger has existed since 00002 and nothing has ever read a row it
    // wrote. Asserted here so the rest of this file is testing a real queue.
    expect(outboxA, 'no outbox row was enqueued for the finding').not.toBe('')
    expect(outboxB).not.toBe('')
    expect(outboxA).not.toBe(outboxB)
  })

  it('leaves the recipient unresolved, as designed', async () => {
    // `user_id` is nullable and the trigger never fills it, because an
    // organisation has members rather than "a user". Reintroducing a recipient
    // at enqueue time to make a query simpler is the thing ENT-192's as-built
    // note warns against.
    const r = await migrator.query(
      `select user_id from notification_outbox where id = $1`,
      [outboxA],
    )
    expect(r.rows[0].user_id).toBeNull()
  })
})

describe.skipIf(!reachable)('the dispatcher role', () => {
  it('reads pending rows across every organisation', async () => {
    // No tenant GUC is set on this connection. That is the point: a delivery
    // loop has no organisation of its own, and if somebody later "hardens"
    // this with an org predicate, delivery stops everywhere silently.
    const r = await agent.query(
      `select id from notification_outbox where status = 'pending' and id = any($1)`,
      [[outboxA, outboxB]],
    )
    expect(r.rows).toHaveLength(2)
  })

  it('can mark a doorbell answered', async () => {
    const r = await agent.query(
      `update notification_outbox
          set status = 'sent', sent_at = now(), attempts = attempts + 1
        where id = $1 and status = 'pending'
        returning status`,
      [outboxB],
    )
    expect(r.rowCount, 'the dispatcher could not record a delivery').toBe(1)
    expect(r.rows[0].status).toBe('sent')
  })

  it('still cannot ring a doorbell for an organisation it is not pointed at', async () => {
    // The half that must NOT widen. Enqueue stays scoped by the existing
    // `notification_outbox_agent` policy to the organisation the GUC names, so
    // a dispatcher that can read everything still cannot fabricate a
    // notification for a tenant it was never given work for.
    await setTenant(agent, orgA, ada)
    const error = await refused(
      agent,
      `insert into notification_outbox (finding_id, org_id) values ($1, $2)`,
      [findingB, orgB],
    )
    expect(
      error,
      'the dispatcher enqueued for another organisation',
    ).not.toBeNull()
    expect(error?.code).toBe('42501')
  })
})

describe.skipIf(!reachable)('notification_recipients', () => {
  it('returns the members of the row organisation and nobody else', async () => {
    const r = await agent.query(`select * from notification_recipients($1)`, [
      outboxA,
    ])
    const users = r.rows.map((row) => row.user_id)

    expect(users, 'Ada should hear about her own organisation').toContain(ada)
    expect(users, 'Bob is in another organisation entirely').not.toContain(bob)
  })

  it('omits somebody with no address rather than returning a row nobody can send', async () => {
    // Miko is a genuine member who has never signed in, so has no identity and
    // no address. Returning them would hand the caller a row it cannot act on.
    const r = await agent.query(`select * from notification_recipients($1)`, [
      outboxA,
    ])
    expect(r.rows.map((row) => row.user_id)).not.toContain(miko)
  })

  it('defaults somebody with no preferences row to hearing about things', async () => {
    // The product default, and it matters more than it looks: requiring a row
    // would mean silence for every member of every organisation until each
    // opted in, which for a compliance product means a critical finding nobody
    // hears about.
    const r = await agent.query(
      `select min_severity, timezone from notification_recipients($1) where user_id = $2`,
      [outboxA, ada],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].min_severity).toBe('medium')
    expect(r.rows[0].timezone).toBe('Europe/Tallinn')
  })

  it('carries the finding severity so the caller can compare', async () => {
    // The function fetches and does not decide. Filtering here would put the
    // "should this person be emailed" rule in plpgsql, where it is hard to test
    // and easy to disagree with silently (§14.5).
    const r = await agent.query(
      `select finding_severity from notification_recipients($1) limit 1`,
      [outboxA],
    )
    expect(r.rows[0].finding_severity).toBe('high')
  })

  it('prefers an address the person chose over the one they sign in with', async () => {
    await migrator.query(
      `insert into notification_preferences (org_id, user_id, email)
       values ($1, $2, 'ada-elsewhere@example.invalid')
       on conflict (org_id, user_id) do update set email = excluded.email`,
      [orgA, ada],
    )
    const r = await agent.query(
      `select email from notification_recipients($1) where user_id = $2`,
      [outboxA, ada],
    )
    expect(r.rows[0].email).toBe('ada-elsewhere@example.invalid')

    await migrator.query(
      `delete from notification_preferences where org_id = $1 and user_id = $2`,
      [orgA, ada],
    )
  })

  it('returns nothing for a row that does not exist', async () => {
    // The function takes an outbox id and nothing else: no org id, no user id,
    // no predicate a caller could widen. So the only way to ask it about
    // somebody is to already hold a due row about them, and asking about a row
    // that is not there answers with silence rather than an error that would
    // distinguish "no such row" from "nobody wants it".
    const r = await agent.query(`select * from notification_recipients($1)`, [
      randomUUID(),
    ])
    expect(r.rows).toHaveLength(0)
  })

  it('returns only what delivery needs, and no identity beyond the address', async () => {
    // The narrow-projection condition. This function exists so the agent does
    // not need grants on memberships, preferences and identities; returning a
    // display name, a role or a membership row would give back through the
    // window what the missing grants keep out of the door, one outbox row at a
    // time.
    const r = await agent.query(`select * from notification_recipients($1)`, [
      outboxA,
    ])
    expect(Object.keys(r.rows[0]).sort()).toEqual(
      [
        'email',
        // Added by 00027, and it is a fact about the address on the line above
        // rather than a new fact about the person. The act-from-email link
        // carries authority to approve a finding, so it may only be sent to an
        // address the IdP said was verified, and the dispatcher has to be able
        // to tell before it renders the message. Anything wider than a boolean
        // about this one address would reopen the argument the narrow
        // projection exists to close.
        'email_verified',
        'finding_severity',
        // The channel half, added by 00044 (ENT-263), and it is the same
        // shape of fact as `email_verified` above: what this person chose,
        // where they can be reached on it, and whether they proved they hold
        // it. Three raw facts and no decision, because the decision (an
        // unverified chat is never delivered to) is `notify.RouteFor`'s and is
        // a Go table test rather than a `where` clause nobody can break
        // deliberately.
        //
        // It stays inside the narrow projection's argument. A chat id is
        // reachability for the person this row is already about, in the
        // organisation the outbox row already names, which is the same class
        // of fact as their address. What would reopen the argument is
        // returning it for anybody the notification is not for.
        'finding_channel',
        'min_severity',
        'org_name',
        'org_slug',
        'quiet_hours_end',
        'quiet_hours_start',
        'telegram_chat_id',
        'telegram_verified',
        'timezone',
        'user_id',
      ].sort(),
    )
  })

  it('is not executable by the application role', async () => {
    // The narrow-grant argument depends on this. If the app could call it, the
    // function would be a way around every policy on memberships and
    // identities rather than a way to avoid granting them.
    const error = await refused(
      app,
      `select * from notification_recipients($1)`,
      [outboxA],
    )
    expect(error, 'the application could resolve recipients').not.toBeNull()
  })
})

describe.skipIf(!reachable)('capability tokens', () => {
  it('redeeming unsubscribes, and creates the preferences row if there is none', async () => {
    // The person most likely to unsubscribe is the one who has never opened the
    // settings page, so has no row. An UPDATE touching zero rows would report
    // success and change nothing, which is the worst outcome for this button.
    const hash = `hash-${randomUUID()}`
    await mintToken(orgA, ada, hash)

    const r = await app.query(
      `select redeem_capability_token($1, 'unsubscribe') as org`,
      [hash],
    )
    expect(r.rows[0].org, 'redemption did not return the organisation').toBe(
      orgA,
    )

    const prefs = await migrator.query(
      `select weekly_briefing_enabled, deadline_alerts_enabled, min_severity_for_email
         from notification_preferences where org_id = $1 and user_id = $2`,
      [orgA, ada],
    )
    expect(prefs.rows).toHaveLength(1)
    expect(prefs.rows[0].weekly_briefing_enabled).toBe(false)
    expect(prefs.rows[0].deadline_alerts_enabled).toBe(false)
    expect(prefs.rows[0].min_severity_for_email).toBe('critical')

    await migrator.query(
      `delete from notification_preferences where org_id = $1 and user_id = $2`,
      [orgA, ada],
    )
  })

  it('is single use', async () => {
    const hash = `hash-${randomUUID()}`
    await mintToken(orgA, ada, hash)

    const first = await app.query(
      `select redeem_capability_token($1, 'unsubscribe') as org`,
      [hash],
    )
    expect(first.rows[0].org).toBe(orgA)

    const second = await app.query(
      `select redeem_capability_token($1, 'unsubscribe') as org`,
      [hash],
    )
    expect(second.rows[0].org, 'a token was redeemed twice').toBeNull()

    await migrator.query(
      `delete from notification_preferences where org_id = $1 and user_id = $2`,
      [orgA, ada],
    )
  })

  it('refuses an expired token', async () => {
    const hash = `hash-${randomUUID()}`
    await mintToken(orgA, ada, hash, 'unsubscribe', '-1 hour')

    const r = await app.query(
      `select redeem_capability_token($1, 'unsubscribe') as org`,
      [hash],
    )
    expect(r.rows[0].org).toBeNull()
  })

  it('answers identically for expired, used, wrong kind and never existed', async () => {
    // The oracle property. A caller with no session must not be able to learn
    // which tokens are real by comparing responses, so all four are one answer.
    const used = `hash-${randomUUID()}`
    await mintToken(orgA, ada, used)
    await app.query(`select redeem_capability_token($1, 'unsubscribe')`, [used])

    const expired = `hash-${randomUUID()}`
    await mintToken(orgA, ada, expired, 'unsubscribe', '-1 hour')

    const wrongKind = `hash-${randomUUID()}`
    await mintToken(orgA, ada, wrongKind)

    const answers = await Promise.all(
      [
        [used, 'unsubscribe'],
        [expired, 'unsubscribe'],
        [wrongKind, 'something-else'],
        [`hash-${randomUUID()}`, 'unsubscribe'],
      ].map(async ([hash, kind]) => {
        const r = await app.query(
          `select redeem_capability_token($1, $2) as org`,
          [hash, kind],
        )
        return r.rows[0].org
      }),
    )

    expect(answers, 'the four cases are distinguishable').toEqual([
      null,
      null,
      null,
      null,
    ])

    await migrator.query(
      `delete from notification_preferences where org_id = $1 and user_id = $2`,
      [orgA, ada],
    )
  })

  it('is not readable by the application role, and fails loudly rather than emptily', async () => {
    // The grant is revoked explicitly rather than left to the absence of a
    // policy, so this is a permission error and not an empty result.
    //
    // That distinction is the test. 00002's default privileges hand the
    // application DML on every table the migrator creates, so without the
    // revoke this table would arrive readable, and FORCE RLS with no policy
    // would turn every select into zero rows. A caller would see a table it can
    // address that happens to be empty, which is indistinguishable from a
    // working boundary right up until somebody adds a policy for an unrelated
    // reason. Asserting on the error code is what pins the loud version.
    await setTenant(app, orgA, ada)
    const error = await refused(app, `select id from capability_tokens`)
    expect(error, 'the application could read capability tokens').not.toBeNull()
    expect(
      error?.code,
      'the table is reachable but empty rather than refused',
    ).toBe('42501')
  })
})

/**
 * The second channel, and the two things about it that are the schema's
 * (ENT-263, migration 00044).
 *
 * The first is that a linked chat is PERSONAL WITHIN AN ORGANISATION, like
 * notification_preferences and unlike almost everything else here. Members
 * share an organisation and do not share a messenger account, so the policies
 * pin `user_id` to the GUC on every command rather than only on the writes.
 * Without that, `notification_channels` would be an endpoint for enumerating
 * which of your colleagues can be reached on Telegram and at which chat.
 *
 * The second is the state machine. A pending code and a `verified_at` are the
 * two halves of one state, and the check constraint refuses a row holding both,
 * because such a row would let a stale code re-verify a channel somebody has
 * already proved. That is an invariant, so it is in the database; the rule that
 * decides whether to deliver to a chat is a decision, so it is in Go, and
 * neither is in the other place.
 */
describe.skipIf(!reachable)('notification_channels', () => {
  const chat = '987654321'

  it('is readable by its owner and invisible to a colleague', async () => {
    await setTenant(app, orgA, ada)
    await app.query(
      `insert into notification_channels (org_id, user_id, kind, chat_id, verified_at)
       values ($1, $2, 'telegram', $3, now())`,
      [orgA, ada, chat],
    )

    const mine = await app.query(
      `select chat_id from notification_channels where kind = 'telegram'`,
    )
    expect(mine.rows.map((r) => r.chat_id)).toEqual([chat])

    // Miko is a genuine member of the same organisation. A colleague reading
    // zero rows is the property; a colleague reading one is a directory of
    // everybody's messaging identity.
    await setTenant(app, orgA, miko)
    const theirs = await app.query(
      `select chat_id from notification_channels where kind = 'telegram'`,
    )
    expect(
      theirs.rows,
      "a colleague could read somebody else's linked chat",
    ).toHaveLength(0)

    // And cannot delete what they cannot see, which is the same policy shape
    // on the other command: unlinking somebody else's chat would be a way to
    // silence a colleague's compliance notifications.
    const deleted = await app.query(
      `delete from notification_channels where kind = 'telegram'`,
    )
    expect(
      deleted.rowCount,
      "a colleague could unlink somebody else's chat",
    ).toBe(0)

    await setTenant(app, orgA, ada)
    await app.query(`delete from notification_channels where kind = 'telegram'`)
  })

  it('refuses a row claiming to be verified while a code is outstanding', async () => {
    await setTenant(app, orgA, ada)
    const error = await refused(
      app,
      `insert into notification_channels
         (org_id, user_id, kind, chat_id,
          verification_code_hash, verification_expires_at, verified_at)
       values ($1, $2, 'telegram', $3, 'deadbeef', now() + interval '10 minutes', now())`,
      [orgA, ada, chat],
    )
    expect(
      error,
      'a verified channel was allowed to hold a pending code, so a kept code ' +
        'could re-verify a chat somebody has already proved',
    ).not.toBeNull()
    expect(error?.code, 'refused for the wrong reason').toBe('23514')
  })

  it('refuses a code that never expires', async () => {
    await setTenant(app, orgA, ada)
    const error = await refused(
      app,
      `insert into notification_channels
         (org_id, user_id, kind, chat_id, verification_code_hash)
       values ($1, $2, 'telegram', $3, 'deadbeef')`,
      [orgA, ada, chat],
    )
    expect(
      error,
      'a verification code with no expiry was accepted, which is the whole ' +
        'point of an expiry',
    ).not.toBeNull()
    expect(error?.code, 'refused for the wrong reason').toBe('23514')
  })

  it('lets the dispatcher see the chat and whether it was proved, separately', async () => {
    // The function fetches; Go decides. Returning the chat id only when
    // `verified_at` is set would put the product rule "an unverified chat is
    // not delivered to" in plpgsql, where it is exercisable only through a
    // live stack and where a later reader can disagree with it without
    // anything going red.
    await setTenant(app, orgA, ada)
    await app.query(
      `insert into notification_channels (org_id, user_id, kind, chat_id)
       values ($1, $2, 'telegram', $3)`,
      [orgA, ada, chat],
    )

    const r = await agent.query(
      `select telegram_chat_id, telegram_verified, finding_channel
         from notification_recipients($1) where user_id = $2`,
      [outboxA, ada],
    )
    expect(r.rows[0].telegram_chat_id).toBe(chat)
    expect(
      r.rows[0].telegram_verified,
      'an unverified chat was reported as proved',
    ).toBe(false)
    expect(
      r.rows[0].finding_channel,
      'somebody who has never chosen a channel is on email',
    ).toBe('email')

    await setTenant(app, orgA, ada)
    await app.query(`delete from notification_channels where kind = 'telegram'`)
  })

  it('reaches somebody who has a chat and no address', async () => {
    // The address filter had to be relaxed at the same time. It read "exclude
    // anybody with no email address", which was right when email was the only
    // channel and silently drops somebody who deliberately chose Telegram.
    // Miko is that person: a member who has never signed in, so no identity
    // row and no address.
    await setTenant(app, orgA, miko)
    await app.query(
      `insert into notification_channels (org_id, user_id, kind, chat_id, verified_at)
       values ($1, $2, 'telegram', $3, now())`,
      [orgA, miko, '112233445'],
    )

    const r = await agent.query(
      `select telegram_chat_id, email from notification_recipients($1) where user_id = $2`,
      [outboxA, miko],
    )
    expect(
      r.rows,
      'somebody reachable only on Telegram was dropped as having no address',
    ).toHaveLength(1)
    expect(r.rows[0].telegram_chat_id).toBe('112233445')

    await setTenant(app, orgA, miko)
    await app.query(`delete from notification_channels where kind = 'telegram'`)
  })
})
