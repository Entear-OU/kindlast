/**
 * Acting for a person: the delegation table's boundary (ENT-230, 00021).
 *
 * A delegation is what lets an agent read and write as the person who asked,
 * without that agent ever holding the person's token. It is therefore the one
 * row in this schema whose whole purpose is to carry authority, so the question
 * this suite exists to answer is narrow and unpleasant: can the thing that
 * mints one mint it for somebody else?
 *
 * The answer has to be no in Postgres rather than in Go, because a bug in the
 * minting handler is exactly the bug an attacker would be looking for. So the
 * with-check on `act_delegations_mint` pins the row to the two tenancy GUCs:
 * the application can only ever write a delegation for the person whose session
 * is open right now, in the organisation they are acting in, and only while
 * they are still a member of it. A handler that got the user id from a request
 * field would be refused by the database rather than obeyed.
 *
 * The other half is redemption, which happens with NO GUCs set, because the
 * caller presenting a delegation has no session yet: resolving it is what
 * decides whose session it becomes. That is why `resolve_act_delegation` is
 * SECURITY DEFINER, the same structural argument `redeem_capability_token`
 * rests on (00015), and why every unusable delegation gets one answer.
 *
 * PROVEN ABLE TO FAIL. Two deliberate breakages, both reverted:
 *
 *   - Dropping `user_id = app_current_user_id()` from the mint policy's with
 *     check turns "cannot mint for somebody else" green-to-red, while every
 *     other test here stays green. That is the shape that says these test the
 *     boundary rather than the plumbing.
 *   - Removing the `revoked_at is null` predicate from resolve_act_delegation
 *     turns the revocation test red on its own.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import { createHash } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  setTenant,
  isStackReachable,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID() // owner of Alpha
const miko = randomUUID() // member of Alpha
const bob = randomUUID() // owner of Beta, in neither of Ada's rows

let migrator: Client
let app: Client

/** The same digest the Go store computes, so the two halves cannot drift. */
function hash(token: string): string {
  return createHash('sha256').update(token).digest('hex')
}

async function seedOrg(org: string, label: string, members: string[]) {
  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `${label} ${org.slice(0, 8)}`],
  )
  for (const user of members) {
    await migrator.query(
      `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
      [org, user],
    )
  }
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  await seedOrg(orgA, 'Delegation Alpha', [ada, miko])
  await seedOrg(orgB, 'Delegation Beta', [bob])
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from act_delegations where org_id = any($1)`, [
    [orgA, orgB],
  ])
  await migrator.query(`delete from organisations where id = any($1)`, [
    [orgA, orgB],
  ])
  await migrator.end()
  await app.end()
})

describe.skipIf(!reachable)('minting a delegation', () => {
  it('is bound to the person whose session is open', async () => {
    await setTenant(app, orgA, ada)

    await app.query(
      `insert into act_delegations
         (id, org_id, user_id, acting_agent, token_hash, expires_at)
       values ($1, $2, $3, 'analyst', $4, now() + interval '10 minutes')`,
      [randomUUID(), orgA, ada, hash(randomUUID())],
    )

    const { rows } = await migrator.query(
      `select count(*)::int as n from act_delegations where org_id = $1 and user_id = $2`,
      [orgA, ada],
    )
    expect(rows[0].n).toBe(1)
  })

  it('cannot be minted for somebody else in the same organisation', async () => {
    // The whole point. Ada is signed in; a handler that took the user id from a
    // request field would try exactly this insert, and the database refuses it
    // rather than trusting the caller.
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into act_delegations
           (id, org_id, user_id, acting_agent, token_hash, expires_at)
         values ($1, $2, $3, 'analyst', $4, now() + interval '10 minutes')`,
        [randomUUID(), orgA, miko, hash(randomUUID())],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('cannot be minted into another organisation', async () => {
    await setTenant(app, orgA, ada)

    await expect(
      app.query(
        `insert into act_delegations
           (id, org_id, user_id, acting_agent, token_hash, expires_at)
         values ($1, $2, $3, 'analyst', $4, now() + interval '10 minutes')`,
        [randomUUID(), orgB, ada, hash(randomUUID())],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('cannot outlive the ceiling, and the ceiling binds the migrator too', async () => {
    // A constraint rather than a Go check, because "no delegation is ever long
    // lived" has to hold no matter who writes. The migrator bypasses RLS and
    // holds every grant, so a check constraint is the only thing that reaches
    // it, which is the argument 00019's append-only trigger already makes.
    await expect(
      migrator.query(
        `insert into act_delegations
           (id, org_id, user_id, acting_agent, token_hash, expires_at)
         values ($1, $2, $3, 'analyst', $4, now() + interval '2 hours')`,
        [randomUUID(), orgA, ada, hash(randomUUID())],
      ),
    ).rejects.toThrow(/act_delegations_ttl/)
  })

  it('names an agent in a shape a customer can be shown', async () => {
    // The value lands in an audit row a customer reads. Free text there would
    // put whatever a caller sent in front of a person as though this system
    // vouched for it.
    await expect(
      migrator.query(
        `insert into act_delegations
           (id, org_id, user_id, acting_agent, token_hash, expires_at)
         values ($1, $2, $3, 'Robert''); drop table findings; --', $4, now() + interval '5 minutes')`,
        [randomUUID(), orgA, ada, hash(randomUUID())],
      ),
    ).rejects.toThrow(/act_delegations_acting_agent/)
  })
})

describe.skipIf(!reachable)('reading delegations back', () => {
  it('shows a person their own and nobody else', async () => {
    const mine = randomUUID()
    const theirs = randomUUID()
    await migrator.query(
      `insert into act_delegations
         (id, org_id, user_id, acting_agent, token_hash, expires_at)
       values ($1, $2, $3, 'analyst', $4, now() + interval '5 minutes'),
              ($5, $2, $6, 'analyst', $7, now() + interval '5 minutes')`,
      [mine, orgA, ada, hash(mine), theirs, miko, hash(theirs)],
    )

    await setTenant(app, orgA, ada)
    const { rows } = await app.query(
      `select id from act_delegations where id = any($1)`,
      [[mine, theirs]],
    )
    expect(rows.map((r) => r.id)).toEqual([mine])
  })

  it('never yields the token itself, only its digest', async () => {
    // Not an assertion about a query, an assertion about the schema: there is
    // no column holding the credential, so a dump, a backup or a support
    // engineer reading this table gets nothing that opens a door.
    const { rows } = await migrator.query(
      `select column_name from information_schema.columns
        where table_schema = 'public' and table_name = 'act_delegations'`,
    )
    const columns = rows.map((r) => r.column_name)
    expect(columns).toContain('token_hash')
    expect(columns).not.toContain('token')
  })
})

describe.skipIf(!reachable)('resolving a delegation', () => {
  async function mint(
    org: string,
    user: string,
    options: { expired?: boolean; revoked?: boolean; singleUse?: boolean } = {},
  ): Promise<string> {
    const token = randomUUID()
    // An expired row is backdated rather than minted with a past expiry: the
    // TTL constraint refuses `expires_at <= created_at`, which is itself the
    // point, so the fixture has to age a legitimate delegation rather than
    // write an impossible one.
    await migrator.query(
      `insert into act_delegations
         (id, org_id, user_id, acting_agent, token_hash, single_use,
          created_at, expires_at, revoked_at)
       values ($1, $2, $3, 'analyst', $4, $5,
               case when $6 then now() - interval '30 minutes' else now() end,
               case when $6 then now() - interval '1 minute'
                    else now() + interval '10 minutes' end,
               case when $7 then now() end)`,
      [
        randomUUID(),
        org,
        user,
        hash(token),
        options.singleUse ?? false,
        options.expired ?? false,
        options.revoked ?? false,
      ],
    )
    return token
  }

  async function resolve(token: string) {
    // No GUCs set on purpose: this is what a caller with no session does, and
    // it is the reason the function is SECURITY DEFINER at all.
    const { rows } = await app.query(
      `select user_id, org_id, acting_agent from resolve_act_delegation($1)`,
      [hash(token)],
    )
    return rows
  }

  it('answers with the person and their organisation', async () => {
    const token = await mint(orgA, ada)
    const rows = await resolve(token)
    expect(rows).toHaveLength(1)
    expect(rows[0].user_id).toBe(ada)
    expect(rows[0].org_id).toBe(orgA)
    expect(rows[0].acting_agent).toBe('analyst')
  })

  it('answers identically for expired, revoked and never existed', async () => {
    const expired = await mint(orgA, ada, { expired: true })
    const revoked = await mint(orgA, ada, { revoked: true })

    expect(await resolve(expired)).toHaveLength(0)
    expect(await resolve(revoked)).toHaveLength(0)
    expect(await resolve(randomUUID())).toHaveLength(0)
  })

  it('resolves a single-use delegation exactly once', async () => {
    const token = await mint(orgA, ada, { singleUse: true })

    expect(await resolve(token)).toHaveLength(1)
    expect(await resolve(token)).toHaveLength(0)
  })

  it('resolves a run delegation as many times as the run needs', async () => {
    // An agent's tools are many calls under one delegation. Single use is the
    // email link's property (ENT-230's second consumer), not this one's, and
    // conflating them would break the rail on its second tool call.
    const token = await mint(orgA, ada)

    expect(await resolve(token)).toHaveLength(1)
    expect(await resolve(token)).toHaveLength(1)
  })

  it('is not readable by the application without the function', async () => {
    // Resolution is the only way in. If the app could select by hash it could
    // also enumerate, and a caller that has proved nothing would be able to
    // learn which digests are real by comparing answers.
    const token = await mint(orgA, ada)
    await setTenant(app, orgA, miko)

    const { rows } = await app.query(
      `select id from act_delegations where token_hash = $1`,
      [hash(token)],
    )
    expect(rows).toHaveLength(0)
  })
})

describe.skipIf(!reachable)('the audit row names the agent', () => {
  it('records what app.acting_agent says, alongside the person', async () => {
    // The delegated transaction spells the agent into the session, and the
    // column's default picks it up. Nothing in record_audit_log had to change,
    // which is the point: a writer that never read 00021 still produces a row
    // naming both.
    await setTenant(app, orgA, ada)
    await app.query("select set_config('app.acting_agent', 'analyst', false)")

    const { rows } = await app.query(
      `select record_audit_log($1, $2, null, 'delegation_test', 'act_delegations',
                               null, null, null, $2) as id`,
      [orgA, ada],
    )

    const stored = await migrator.query(
      `select user_id, acting_agent from audit_log where id = $1`,
      [rows[0].id],
    )
    expect(stored.rows[0].user_id).toBe(ada)
    expect(stored.rows[0].acting_agent).toBe('analyst')

    await migrator.query(`delete from audit_log where id = $1`, [rows[0].id])
  })

  it('is null when a person acted for themselves', async () => {
    // A column that was never null would be useless: "an agent did this" only
    // means something if the ordinary case says nothing.
    await setTenant(app, orgA, ada)
    await app.query("select set_config('app.acting_agent', '', false)")

    const { rows } = await app.query(
      `select record_audit_log($1, $2, null, 'delegation_test', 'act_delegations',
                               null, null, null, $2) as id`,
      [orgA, ada],
    )

    const stored = await migrator.query(
      `select acting_agent from audit_log where id = $1`,
      [rows[0].id],
    )
    expect(stored.rows[0].acting_agent).toBeNull()

    await migrator.query(`delete from audit_log where id = $1`, [rows[0].id])
  })
})
