/**
 * Co-member identity visibility (ENT-202).
 *
 * `user_identities` was self-only when 00003 created it, and that was
 * deliberate: identity is not tenant-scoped, one human is the same person in
 * every organisation they belong to, and the table holds personal data.
 *
 * Building the members settings surface made the cost of that visible. Under a
 * self-only policy, listing an organisation's members returns uuids and roles
 * and nothing else, so the page can only offer to remove `3f9a1c72-...`. The
 * user ruled on 2026-08-15 that members see each other's display name and
 * email, uniformly across all three roles, having been offered and declined a
 * variant that masked email from members and viewers.
 *
 * So this suite exists to pin the *boundary* of that reversal, not merely its
 * happy path. What 00003 was protecting against was cross-tenant identity
 * leakage, and that protection has to survive intact: the new policy grants
 * nothing outside a shared membership. The third and fourth tests are the ones
 * that matter, because a policy that over-grants passes the first two.
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

// Two organisations. Ada and Miko share the first; Bob is alone in the second
// and is the person who must stay invisible.
const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()
const miko = randomUUID()
const bob = randomUUID()

let migrator: Client
let app: Client

async function seedIdentity(
  c: Client,
  userID: string,
  handle: string,
): Promise<void> {
  await c.query(
    `insert into user_identities (user_id, issuer, subject, email, display_name)
     values ($1, 'https://test.invalid', $2, $3, $4)`,
    [userID, `subject-${handle}`, `${handle}@example.invalid`, handle],
  )
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)

  for (const [org, name] of [
    [orgA, 'Co-member Fixture A'],
    [orgB, 'Co-member Fixture B'],
  ] as const) {
    await migrator.query(
      `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
      [org, `${name} ${org.slice(0, 8)}`],
    )
  }

  await migrator.query(
    `insert into memberships (org_id, user_id, role) values
       ($1, $2, 'owner'), ($1, $3, 'viewer'), ($4, $5, 'owner')`,
    [orgA, ada, miko, orgB, bob],
  )

  await seedIdentity(migrator, ada, 'ada')
  await seedIdentity(migrator, miko, 'miko')
  await seedIdentity(migrator, bob, 'bob')
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(
    `delete from user_identities where user_id in ($1,$2,$3)`,
    [ada, miko, bob],
  )
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    orgA,
    orgB,
  ])
  await Promise.all([migrator.end(), app.end()])
})

describe.skipIf(!reachable)('a member can see who their co-members are', () => {
  it('reads a co-member display name and email', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `select display_name, email from user_identities where user_id = $1`,
      [miko],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].display_name).toBe('miko')
    expect(r.rows[0].email).toBe('miko@example.invalid')
  })

  // The user declined role-dependent masking, so a viewer sees exactly what an
  // owner sees. Asserted rather than assumed, because "uniform across roles"
  // is the decision that was taken and a later tightening should have to break
  // a test rather than pass quietly.
  it('applies to a viewer as much as to an owner', async () => {
    await setTenant(app, orgA, miko)
    const r = await app.query(
      `select email from user_identities where user_id = $1`,
      [ada],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].email).toBe('ada@example.invalid')
  })

  it('still reads its own row', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `select user_id from user_identities where user_id = $1`,
      [ada],
    )
    expect(r.rows).toHaveLength(1)
  })
})

describe.skipIf(!reachable)('and nobody outside the organisation', () => {
  // The whole point of the reversal being narrow. If this passes while the
  // tests above also pass, the policy grants co-membership rather than
  // authentication.
  it('cannot read the identity of someone in another organisation', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `select count(*)::int as n from user_identities where user_id = $1`,
      [bob],
    )
    expect(r.rows[0].n).toBe(0)
  })

  // A middleware bug that sets an organisation the caller does not belong to
  // must not become an identity disclosure, which is the property every other
  // policy in this schema carries.
  //
  // Note what is deliberately NOT asserted here: that she can read nothing at
  // all. She can still read her own row, because user_identities_select_self
  // carries no org clause and 00003 meant that, identity not being
  // tenant-scoped. The first draft of this test asserted zero rows and failed
  // for that reason; the policy was right and the assertion was wrong.
  //
  // The real property is narrower and more interesting: the co-member grant is
  // scoped to the ACTIVE organisation, not to any organisation two people
  // happen to share. Ada and Miko are genuine co-members in orgA, so if this
  // were scoped to "some shared org" rather than "the active org", Miko would
  // leak here.
  it('reads only itself when acting in an organisation it does not belong to', async () => {
    await setTenant(app, orgB, ada)

    const mine = await app.query(
      `select count(*)::int as n from user_identities where user_id = $1`,
      [ada],
    )
    expect(mine.rows[0].n, 'own identity stays readable, per 00003').toBe(1)

    const coMember = await app.query(
      `select count(*)::int as n from user_identities where user_id = $1`,
      [miko],
    )
    expect(
      coMember.rows[0].n,
      'a real co-member from another org must not leak through the active org',
    ).toBe(0)

    const everyone = await app.query(
      `select count(*)::int as n from user_identities`,
    )
    expect(everyone.rows[0].n, 'exactly one row: her own').toBe(1)
  })
})

describe.skipIf(!reachable)('writes stay self-only', () => {
  // Visibility was widened. Authority was not, and conflating the two is the
  // easy mistake when relaxing a policy: `for all` instead of `for select`
  // would let any co-member rewrite another person's email address.
  it('a co-member cannot update another identity', async () => {
    await setTenant(app, orgA, ada)
    const r = await app.query(
      `update user_identities set display_name = 'hijacked' where user_id = $1`,
      [miko],
    )
    expect(r.rowCount).toBe(0)

    const check = await migrator.query(
      `select display_name from user_identities where user_id = $1`,
      [miko],
    )
    expect(check.rows[0].display_name).toBe('miko')
  })
})
