/**
 * Accepting an invitation writes an audit row, and only the definer function
 * can write it (ENT-268).
 *
 * ENT-255 put four of the five membership actions into the audit log and left
 * this one out, for a reason that is a property of the policy surface rather
 * than a matter of time. `audit_log_insert_org` requires the row's `org_id` to
 * equal `app.current_org_id`, the row's `user_id` to equal
 * `app.current_user_id`, and a membership to exist for that pair. Somebody
 * redeeming an invitation has no active organisation and no membership: that is
 * what an invitation is for. So the row cannot be written beside the
 * acceptance, and 00038 writes it inside `accept_invitation`, which is SECURITY
 * DEFINER, in the same statement block that creates the membership.
 *
 * # WHY THIS SUITE AND NOT ONLY THE GO STORE TESTS
 *
 * Because the interesting half is what `kindlast_app` may and may not do, and
 * the Go tests exercise only the path that is meant to work. A change that made
 * this row possible by loosening `audit_log_insert_org` would leave every Go
 * test green while opening the organisation's regulatory record to writes from
 * a caller who is not in it. So the assertions here are paired: the definer
 * function writes the row, AND the same app-role session is still refused when
 * it tries to write the same row directly.
 *
 * The second pair is tenancy. The row has to be readable by the organisation it
 * belongs to and invisible to every other one, because an audit row that landed
 * where nobody can see it answers nobody, and one that landed where the wrong
 * people can see it is a tenant leak on the single table a customer's regulator
 * will read.
 *
 * Everything runs as `kindlast_app` except the fixture seeding and the
 * counting, which are the migrator's job precisely so the assertions can tell
 * "no such row" apart from "a row this caller may not see".
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

// The organisation somebody is invited into, and an unrelated one whose member
// must never see the row.
const host = randomUUID()
const stranger = randomUUID()

const owner = randomUUID() // already in `host`, and the one who invited
const joiner = randomUUID() // holds the invitation, is in nothing yet
const outsider = randomUUID() // in `stranger` only

const NO_ORGANISATION = '00000000-0000-0000-0000-000000000000'

/** The same hash Go's HashInvitationToken writes. Only the hash is ever stored. */
function tokenHash(token: string): string {
  return createHash('sha256').update(token).digest('hex')
}

let migrator: Client
let app: Client

/** Seeds an invitation and returns its id. */
async function invite(
  orgId: string,
  email: string,
  role: string,
  token: string,
  validFor = '1 hour',
): Promise<string> {
  const { rows } = await migrator.query(
    `insert into invitations (org_id, email, role, token_hash, invited_by, expires_at)
     values ($1, $2, $3, $4, $5, now() + $6::interval)
     returning id`,
    [orgId, email, role, tokenHash(token), owner, validFor],
  )
  return rows[0].id
}

/** Acceptance rows for one invitation, counted outside RLS. */
async function acceptanceRows(invitationId: string): Promise<number> {
  const { rows } = await migrator.query(
    `select count(*)::int as n from audit_log
     where action_type = 'accept_invitation' and target_id = $1`,
    [invitationId],
  )
  return rows[0].n
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  for (const [id, name] of [
    [host, 'Acceptance Audit Host'],
    [stranger, 'Acceptance Audit Stranger'],
  ] as const) {
    await migrator.query(
      `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
      [id, `${name} ${id.slice(0, 8)}`],
    )
  }

  await migrator.query(
    `insert into memberships (org_id, user_id, role) values
       ($1, $2, 'owner'), ($3, $4, 'owner')`,
    [host, owner, stranger, outsider],
  )
})

afterAll(async () => {
  if (!reachable) return

  // `audit_log` forbids UPDATE by trigger and says nothing about DELETE, which
  // is what lets the migrator take fixture rows back out. The application role
  // holds no delete grant on it, so the append-only property the product
  // depends on is unaffected by this.
  await migrator.query(`delete from audit_log where org_id = any($1)`, [
    [host, stranger],
  ])
  await migrator.query(`delete from invitations where org_id = any($1)`, [
    [host, stranger],
  ])
  await migrator.query(`delete from memberships where org_id = any($1)`, [
    [host, stranger],
  ])
  await migrator.query(`delete from organisations where id = any($1)`, [
    [host, stranger],
  ])

  await migrator.end()
  await app.end()
})

describe.skipIf(!reachable)('accepting an invitation is audited', () => {
  it('writes the row from a caller who belongs to no organisation yet', async () => {
    const token = `db-accept-${randomUUID()}`
    const invitationId = await invite(
      host,
      'arrives@example.invalid',
      'member',
      token,
    )

    // The GUCs a real acceptance carries. There is no organisation to name:
    // §1.8 redeems the invitation before the first GetCurrentUser, so the
    // caller resolves to the no-organisation sentinel. This is exactly the
    // state in which `audit_log_insert_org` cannot be satisfied.
    await setTenant(app, NO_ORGANISATION, joiner)

    const { rows } = await app.query(
      `select accept_invitation($1, $2) as org_id`,
      [tokenHash(token), 'arrives@example.invalid'],
    )
    expect(rows[0].org_id).toBe(host)

    // Written into the organisation joined, naming the joiner, carrying the
    // role granted and nothing before it. The role snapshot in `actor_role` is
    // what proves the membership already existed when the row was written: it
    // is read out of `memberships` by `record_audit_log`, so a null there means
    // the log recorded an arrival without recording at what authority.
    const written = await migrator.query(
      `select org_id, user_id, actor_role, target_table, before, after
       from audit_log
       where action_type = 'accept_invitation' and target_id = $1`,
      [invitationId],
    )
    expect(written.rowCount).toBe(1)
    expect(written.rows[0]).toMatchObject({
      org_id: host,
      user_id: joiner,
      actor_role: 'member',
      target_table: 'invitations',
      before: null,
      after: { role: 'member' },
    })

    // No token and no hash anywhere in the row. The audit log is readable by
    // every member and exportable to CSV, and an invitation token is a
    // capability: recorded here it would be quietly re-issued to everybody who
    // can read the log.
    const leaked = await migrator.query(
      `select count(*)::int as n from audit_log
       where action_type = 'accept_invitation'
         and (before::text like '%' || $1 || '%' or after::text like '%' || $1 || '%'
           or before::text like '%' || $2 || '%' or after::text like '%' || $2 || '%')`,
      [token, tokenHash(token)],
    )
    expect(leaked.rows[0].n).toBe(0)
  })

  /**
   * The reason the definer function exists, asserted rather than assumed.
   *
   * If this ever passes, `audit_log_insert_org` has been loosened and any
   * authenticated caller can write into an organisation's regulatory record
   * without being in it. That is a strictly worse outcome than the gap ENT-268
   * closed, so it is worth a test of its own rather than a comment.
   */
  it('still refuses the same row written directly by the app role', async () => {
    const token = `db-accept-direct-${randomUUID()}`
    const invitationId = await invite(
      host,
      'direct@example.invalid',
      'member',
      token,
    )

    const nonMember = randomUUID()
    await setTenant(app, NO_ORGANISATION, nonMember)

    await expect(
      app.query(
        `insert into audit_log
           (org_id, user_id, action_type, target_table, target_id, approving_user_id)
         values ($1, $2, 'accept_invitation', 'invitations', $3, $2)`,
        [host, nonMember, invitationId],
      ),
    ).rejects.toThrow(/row-level security/i)

    // And naming the organisation in the GUC does not help either: the policy's
    // membership `exists` is the half a middleware bug cannot talk its way past.
    await setTenant(app, host, nonMember)
    await expect(
      app.query(
        `insert into audit_log
           (org_id, user_id, action_type, target_table, target_id, approving_user_id)
         values ($1, $2, 'accept_invitation', 'invitations', $3, $2)`,
        [host, nonMember, invitationId],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('shows the row to the organisation it belongs to and to nobody else', async () => {
    const token = `db-accept-visible-${randomUUID()}`
    const invitationId = await invite(
      host,
      'visible@example.invalid',
      'viewer',
      token,
    )

    const newcomer = randomUUID()
    await setTenant(app, NO_ORGANISATION, newcomer)
    await app.query(`select accept_invitation($1, $2)`, [
      tokenHash(token),
      'visible@example.invalid',
    ])

    // The owner who issued the invitation can read the arrival. This is the
    // whole point of the row: the log already said access was offered, and now
    // it says it was taken up.
    await setTenant(app, host, owner)
    const seen = await app.query(
      `select after from audit_log
       where action_type = 'accept_invitation' and target_id = $1`,
      [invitationId],
    )
    expect(seen.rowCount).toBe(1)
    expect(seen.rows[0].after).toEqual({ role: 'viewer' })

    // A member of a different organisation sees nothing, even holding the
    // invitation id. Counted outside RLS first so this is an isolation failure
    // if it ever passes vacuously.
    expect(await acceptanceRows(invitationId)).toBe(1)
    await setTenant(app, stranger, outsider)
    const hidden = await app.query(
      `select 1 from audit_log
       where action_type = 'accept_invitation' and target_id = $1`,
      [invitationId],
    )
    expect(hidden.rowCount).toBe(0)
  })

  /**
   * Nothing happened, so nothing is recorded.
   *
   * Expired, already accepted, never existed and addressed to somebody else are
   * one answer to the caller on purpose (00003, 00033): distinguishing them
   * turns this into an oracle for which tokens are real and who they name. They
   * have to be one answer in the audit log too, and silence is the only one
   * that works. A row on refusal would record an access grant that did not
   * happen, and would let anyone holding a guessed token write into an
   * organisation's regulatory record.
   */
  it.each([
    ['addressed to somebody else', 'mallory@example.invalid'],
    ['no address at all', ''],
  ])('records nothing when refused: %s', async (_name, address) => {
    const token = `db-accept-refused-${randomUUID()}`
    const invitationId = await invite(
      host,
      'refused@example.invalid',
      'owner',
      token,
    )

    const caller = randomUUID()
    await setTenant(app, NO_ORGANISATION, caller)

    const { rows } = await app.query(
      `select accept_invitation($1, $2) as org_id`,
      [tokenHash(token), address],
    )
    expect(rows[0].org_id).toBeNull()

    expect(await acceptanceRows(invitationId)).toBe(0)

    // The invitation survives a refusal, which is 00033's point and is what
    // makes an inviter's curious click harmless. Asserted here because a future
    // change that consumed the row on a mismatch would also want to log it.
    const untouched = await migrator.query(
      `select accepted_at from invitations where id = $1`,
      [invitationId],
    )
    expect(untouched.rows[0].accepted_at).toBeNull()
  })

  it('records nothing for an expired invitation', async () => {
    const token = `db-accept-expired-${randomUUID()}`
    const invitationId = await invite(
      host,
      'expired@example.invalid',
      'member',
      token,
      '-1 hour',
    )

    const caller = randomUUID()
    await setTenant(app, NO_ORGANISATION, caller)

    const { rows } = await app.query(
      `select accept_invitation($1, $2) as org_id`,
      [tokenHash(token), 'expired@example.invalid'],
    )
    expect(rows[0].org_id).toBeNull()
    expect(await acceptanceRows(invitationId)).toBe(0)
  })
})
