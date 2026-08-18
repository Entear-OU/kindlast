/**
 * Approving a finding from a link in an email (ENT-249, migration 00027).
 *
 * WHAT IS ACTUALLY AT RISK HERE
 *
 * This is the one place in the product where authority to make a regulatory
 * decision leaves the building. A delegation minted for the approve link lives
 * in a mailbox, in a mail server's logs, and in whatever gateway scanned the
 * message on the way, so every property below is what stops a credential in
 * that position being worth stealing.
 *
 * Four of them, and they fail in four different ways.
 *
 * BOUND TO ONE FINDING, IN BOTH DIRECTIONS. `resolve_act_delegation` matches
 * the finding with `is not distinct from`, which is symmetrical on purpose. A
 * caller naming no finding cannot resolve an approve link, so the credential
 * cannot be presented in the `Kindlast-Delegation` header as a general session
 * for that person; a caller naming a finding cannot resolve a run delegation,
 * so the rail's credential cannot be spent through the approve endpoint.
 *
 * ONE ANSWER FOR EVERY UNUSABLE CASE. Second redemption, expired, revoked,
 * wrong finding and never existed all return zero rows. Anything that told
 * them apart would make this an oracle for which credentials are real, to a
 * caller who has proved nothing.
 *
 * MINTED FOR A MEMBER, AND ONLY FROM A ROW THE CALLER HOLDS. The dispatcher is
 * the one legitimate minter with no session, so it cannot be trusted with a
 * free choice of organisation, finding and person. It passes an outbox row it
 * has claimed and a user id, and the function derives the rest.
 *
 * NEVER TO AN UNVERIFIED ADDRESS. Section 1.8 gates acting on a finding behind
 * a verified address, and through a link there is no token to carry the claim.
 * The trigger is what makes that hold for the schema owner too, which is what
 * the minting function runs as.
 *
 * PROVEN ABLE TO FAIL. Two deliberate breakages, both reverted:
 *
 *   - Dropping `and d.finding_id is not distinct from p_finding_id` from
 *     `resolve_act_delegation` turns "refuses a different finding" and "cannot
 *     be spent as a session" red together, and leaves every other test here
 *     green.
 *   - Dropping the `if v.single_use` branch turns "redeems exactly once" red on
 *     its own, and nothing else.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID, createHash } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  setTenant,
  isStackReachable,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const AGENT_URL =
  process.env.PG_AGENT_URL ??
  'postgres://kindlast_agent:agent-dev-password@127.0.0.1:5433/kindlast'

const org = randomUUID()
const ada = randomUUID() // owner, signed in, address verified
const miko = randomUUID() // member, signed in, address NOT verified
const obligationID = randomUUID()
const obligationSlug = `act-from-email-${randomUUID().slice(0, 8)}`

let migrator: Client
let app: Client
let agent: Client

let finding = ''
let otherFinding = ''
let outbox = ''

/** The same digest the Go store computes, so the two halves cannot drift. */
function hash(token: string): string {
  return createHash('sha256').update(token).digest('hex')
}

/** A finding with the chain it needs underneath it. Inserting one trips
 *  `enqueue_finding_notification`, so the outbox row arrives by itself. */
async function seedFinding(): Promise<string> {
  const session = randomUUID()
  const profile = randomUUID()
  const signal = randomUUID()
  const id = randomUUID()

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
    `insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
     values ($1, $2, $3, 'profile_gap', 'Fixture signal', $4)`,
    [signal, org, profile, `dedup-${signal}`],
  )
  await migrator.query(
    `insert into findings
       (id, org_id, profile_id, watcher_finding_id, obligation_id, obligation_slug,
        detected, proposed_action, severity)
     values ($1, $2, $3, $4, $5, $6, 'Fixture gap', 'Fixture action', 'high')`,
    [id, org, profile, signal, obligationID, obligationSlug],
  )
  return id
}

async function outboxFor(findingID: string): Promise<string> {
  const r = await migrator.query(
    `select id from notification_outbox where finding_id = $1`,
    [findingID],
  )
  return r.rows[0]?.id ?? ''
}

/** Mint through the dispatcher's own path, as the dispatcher's own role. */
async function mint(
  outboxID: string,
  user: string,
  token: string,
  lifetime = '1 hour',
): Promise<string | null> {
  const r = await agent.query(
    `select mint_finding_approval_delegation($1, $2, $3, $4::interval) as id`,
    [outboxID, user, hash(token), lifetime],
  )
  return r.rows[0].id
}

/** Resolution, with no GUCs set: this is what a caller with no session does,
 *  and it is the reason the function is SECURITY DEFINER at all. */
async function resolve(token: string, findingID: string | null) {
  const { rows } = await app.query(
    `select user_id, org_id, acting_agent from resolve_act_delegation($1, $2::uuid)`,
    [hash(token), findingID],
  )
  return rows
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `Act From Email ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner'), ($1, $3, 'member')`,
    [org, ada, miko],
  )

  // Ada's address is verified and Miko's is not. Both have signed in, so both
  // have an identity row: the difference is only what the IdP said about the
  // address, which is exactly the state the gate exists to distinguish.
  await migrator.query(
    `insert into user_identities (user_id, issuer, subject, email, display_name, email_verified)
     values ($1, 'https://test.invalid', $2, $3, 'ada', true),
            ($4, 'https://test.invalid', $5, $6, 'miko', false)`,
    [
      ada,
      `subject-${ada}`,
      `ada-${ada.slice(0, 8)}@example.invalid`,
      miko,
      `subject-${miko}`,
      `miko-${miko.slice(0, 8)}@example.invalid`,
    ],
  )

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

  finding = await seedFinding()
  otherFinding = await seedFinding()
  outbox = await outboxFor(finding)
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from act_delegations where org_id = $1`, [org])
  await migrator.query(`delete from audit_log where org_id = $1`, [org])
  await migrator.query(`delete from notification_outbox where org_id = $1`, [
    org,
  ])
  await migrator.query(`delete from findings where org_id = $1`, [org])
  await migrator.query(`delete from watcher_findings where org_id = $1`, [org])
  await migrator.query(`delete from compliance_profiles where org_id = $1`, [
    org,
  ])
  await migrator.query(`delete from onboarding_sessions where org_id = $1`, [
    org,
  ])
  await migrator.query(`delete from memberships where org_id = $1`, [org])
  await migrator.query(`delete from user_identities where user_id = any($1)`, [
    [ada, miko],
  ])
  await migrator.query(`delete from obligations where id = $1`, [obligationID])
  await migrator.query(`delete from organisations where id = $1`, [org])
  await migrator.end()
  await app.end()
  await agent.end()
})

describe.skipIf(!reachable)(
  'minting the link the dispatcher puts in a message',
  () => {
    it('binds the delegation to the outbox row it was handed', async () => {
      const id = await mint(outbox, ada, randomUUID())
      expect(id).not.toBeNull()

      const { rows } = await migrator.query(
        `select org_id, user_id, finding_id, acting_agent, single_use, redeemed_at
         from act_delegations where id = $1`,
        [id],
      )
      expect(rows[0].org_id).toBe(org)
      expect(rows[0].user_id).toBe(ada)
      // Derived from the outbox row rather than taken from the caller, so a
      // dispatcher cannot pair a person with a finding it was not sent to
      // deliver.
      expect(rows[0].finding_id).toBe(finding)
      expect(rows[0].acting_agent).toBe('email')
      expect(rows[0].single_use).toBe(true)
      expect(rows[0].redeemed_at).toBeNull()
    })

    it('refuses somebody who is not a member of that organisation', async () => {
      // A stranger, not a co-member: the dispatcher has no business minting for
      // anybody it did not find through `notification_recipients`, and a user id
      // is the one argument a caller could get wrong or be made to get wrong.
      expect(await mint(outbox, randomUUID(), randomUUID())).toBeNull()
    })

    it('refuses an outbox row that does not exist', async () => {
      expect(await mint(randomUUID(), ada, randomUUID())).toBeNull()
    })

    it('refuses an address nobody proved they control', async () => {
      // Section 1.8's gate, held by the table rather than by the caller. Miko is
      // a genuine member who is genuinely reachable; what is missing is the IdP
      // saying the address is theirs.
      await expect(mint(outbox, miko, randomUUID())).rejects.toThrow(
        /no verified address/,
      )
    })

    it('leaves the dispatcher unable to read a delegation back', async () => {
      // The narrow-grant design from 00021 survives this migration: the agent
      // role gains the ability to mint one credential through one function and
      // nothing else. It holds the token it generated; it cannot enumerate.
      const token = randomUUID()
      await mint(outbox, ada, token)

      await expect(
        agent.query(`select id from act_delegations where token_hash = $1`, [
          hash(token),
        ]),
      ).rejects.toThrow(/permission denied/i)
    })

    it('cannot outlive the ceiling, even through the function', async () => {
      await expect(mint(outbox, ada, randomUUID(), '2 hours')).rejects.toThrow(
        /act_delegations_ttl/,
      )
    })
  },
)

describe.skipIf(!reachable)(
  'a finding-bound delegation is single use by construction',
  () => {
    it('is refused as a multi-use row, and the refusal binds the migrator', async () => {
      // Not a habit of the minting code. An approve link that could be redeemed
      // twice would approve, be un-approved by a human, and approve again from
      // the same message.
      await expect(
        migrator.query(
          `insert into act_delegations
           (id, org_id, user_id, acting_agent, token_hash, single_use, finding_id, expires_at)
         values ($1, $2, $3, 'email', $4, false, $5, now() + interval '10 minutes')`,
          [randomUUID(), org, ada, hash(randomUUID()), finding],
        ),
      ).rejects.toThrow(/act_delegations_finding_is_single_use/)
    })

    it('cannot be repointed at a different finding after it is minted', async () => {
      // Without this the update grant that makes revocation possible would also
      // make "revoke" a way to aim a credential already sitting in somebody's
      // mailbox at a different decision.
      const id = await mint(outbox, ada, randomUUID())

      await expect(
        migrator.query(
          `update act_delegations set finding_id = $1 where id = $2`,
          [otherFinding, id],
        ),
      ).rejects.toThrow(/only revoked_at and redeemed_at may change/)
    })
  },
)

describe.skipIf(!reachable)('redeeming it', () => {
  it('answers with the person, their organisation and the channel', async () => {
    const token = randomUUID()
    await mint(outbox, ada, token)

    const rows = await resolve(token, finding)
    expect(rows).toHaveLength(1)
    expect(rows[0].user_id).toBe(ada)
    expect(rows[0].org_id).toBe(org)
    // What ends up in the audit row beside the person. The channel is the
    // answer a reader of the trail needs: it is how they ask whether a link in
    // a mailbox should have been able to do this.
    expect(rows[0].acting_agent).toBe('email')
  })

  it('redeems exactly once', async () => {
    const token = randomUUID()
    await mint(outbox, ada, token)

    expect(await resolve(token, finding)).toHaveLength(1)
    expect(await resolve(token, finding)).toHaveLength(0)
  })

  it('refuses a different finding, and says nothing about why', async () => {
    const token = randomUUID()
    await mint(outbox, ada, token)

    expect(await resolve(token, otherFinding)).toHaveLength(0)
    // And the refusal did not spend it, so the person whose link it is can
    // still use it. A wrong guess must not be a way to burn somebody's link.
    expect(await resolve(token, finding)).toHaveLength(1)
  })

  it('cannot be spent as a session for that person', async () => {
    // The header path resolves with no finding named. If an approve link
    // answered there, a credential minted to approve one finding would be a
    // fifteen minute session with everything that person can do.
    const token = randomUUID()
    await mint(outbox, ada, token)

    expect(await resolve(token, null)).toHaveLength(0)
  })

  it('cannot spend a run delegation through the approve path either', async () => {
    // The other direction of the same rule. A delegation minted for the rail
    // is not bound to a finding, so naming one must not resolve it.
    const token = randomUUID()
    await migrator.query(
      `insert into act_delegations
         (id, org_id, user_id, acting_agent, token_hash, expires_at)
       values ($1, $2, $3, 'analyst', $4, now() + interval '10 minutes')`,
      [randomUUID(), org, ada, hash(token)],
    )

    expect(await resolve(token, finding)).toHaveLength(0)
    expect(await resolve(token, null)).toHaveLength(1)
  })

  it('answers identically for used, expired, revoked, wrong finding and never existed', async () => {
    // The whole set in one place, because the property is that these are
    // indistinguishable rather than that each individually fails.
    const used = randomUUID()
    await mint(outbox, ada, used)
    await resolve(used, finding)

    const expired = randomUUID()
    const revoked = randomUUID()
    // Aged rather than minted with a past expiry: the TTL constraint refuses
    // `expires_at <= created_at`, which is itself the point.
    await migrator.query(
      `insert into act_delegations
         (id, org_id, user_id, acting_agent, token_hash, single_use, finding_id,
          created_at, expires_at, revoked_at)
       values ($1, $2, $3, 'email', $4, true, $5,
               now() - interval '30 minutes', now() - interval '1 minute', null),
              ($6, $2, $3, 'email', $7, true, $5,
               now(), now() + interval '10 minutes', now())`,
      [
        randomUUID(),
        org,
        ada,
        hash(expired),
        finding,
        randomUUID(),
        hash(revoked),
      ],
    )

    const answers = [
      await resolve(used, finding),
      await resolve(expired, finding),
      await resolve(revoked, finding),
      await resolve(randomUUID(), finding),
      await resolve(randomUUID(), randomUUID()),
    ]
    for (const rows of answers) {
      expect(rows).toEqual([])
    }
  })
})

describe.skipIf(!reachable)(
  'what the dispatcher is told about an address',
  () => {
    it('reports a verified sign-in address as verified', async () => {
      const r = await agent.query(
        `select email_verified from notification_recipients($1) where user_id = $2`,
        [outbox, ada],
      )
      expect(r.rows[0].email_verified).toBe(true)
    })

    it('reports an unverified one as unverified rather than omitting the person', async () => {
      // Miko still gets the doorbell. What they do not get is the approve link,
      // and the difference has to be visible to the dispatcher so it can send a
      // message rather than fail a delivery.
      const r = await agent.query(
        `select email_verified from notification_recipients($1) where user_id = $2`,
        [outbox, miko],
      )
      expect(r.rows).toHaveLength(1)
      expect(r.rows[0].email_verified).toBe(false)
    })

    it('reports an override address as unverified even when the sign-in address is not', async () => {
      // `notification_preferences.email` exists so somebody can be told somewhere
      // other than where they sign in, and nobody has proved they control that
      // second address. So the answer is about THIS address rather than about the
      // person, which is what makes the gate correct without a rule in Go.
      await migrator.query(
        `insert into notification_preferences (org_id, user_id, email)
       values ($1, $2, $3)
       on conflict (org_id, user_id) do update set email = excluded.email`,
        [org, ada, `elsewhere-${ada.slice(0, 8)}@example.invalid`],
      )

      const r = await agent.query(
        `select email, email_verified from notification_recipients($1) where user_id = $2`,
        [outbox, ada],
      )
      expect(r.rows[0].email).toContain('elsewhere-')
      expect(r.rows[0].email_verified).toBe(false)

      await migrator.query(
        `delete from notification_preferences where org_id = $1 and user_id = $2`,
        [org, ada],
      )
    })
  },
)

describe.skipIf(!reachable)('the approval it produces', () => {
  it('is the delegated person acting, with the channel named beside them', async () => {
    // The shape the redemption path produces in Go: resolve, set the two
    // tenancy GUCs to the person the delegation names, set `app.acting_agent`
    // to what it says, and then act through the ordinary path. Asserted here
    // over the schema, because the claim is about what the database records
    // rather than about what a handler intended.
    const token = randomUUID()
    await mint(outbox, ada, token)
    const [grant] = await resolve(token, finding)

    await app.query(`select set_config('app.current_user_id', $1, false)`, [
      grant.user_id,
    ])
    await app.query(`select set_config('app.current_org_id', $1, false)`, [
      grant.org_id,
    ])
    await app.query(`select set_config('app.acting_agent', $1, false)`, [
      grant.acting_agent,
    ])

    await app.query(
      `update findings set status = 'approved', approved_by = $2, approval_reviewed = false
         where id = $1 and org_id = $3 and status <> 'approved'`,
      [finding, grant.user_id, grant.org_id],
    )
    await app.query(
      `select record_audit_log($1, $2, $3, 'approve_finding', 'findings', $3, null, null, $2)`,
      [grant.org_id, grant.user_id, finding],
    )

    const { rows } = await migrator.query(
      `select a.user_id, a.approving_user_id, a.acting_agent, f.approved_by, f.status
         from audit_log a join findings f on f.id = a.finding_id
        where a.finding_id = $1 and a.action_type = 'approve_finding'`,
      [finding],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].user_id).toBe(ada)
    expect(rows[0].approving_user_id).toBe(ada)
    expect(rows[0].acting_agent).toBe('email')
    expect(rows[0].approved_by).toBe(ada)
    expect(rows[0].status).toBe('approved')

    await app.query(`select set_config('app.acting_agent', '', false)`)
  })
})
