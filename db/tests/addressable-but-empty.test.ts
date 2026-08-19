/**
 * A table with no policy is not private, it is empty (ENT-210).
 *
 * WHY THIS SWEEP EXISTS
 *
 * 00002 granted `kindlast_app` DML on every table in the schema and set default
 * privileges so it gets the same on every table the migrator creates
 * afterwards. `FORCE ROW LEVEL SECURITY` is on everywhere. So a table with no
 * policy for the application is one the application can address and finds
 * empty: every select returns no rows, every write touches none, and nothing
 * raises.
 *
 * That reads exactly like a boundary. It is not one. The difference matters
 * twice over:
 *
 *   - It fails quietly. A missing grant is a `42501` at parse time, which
 *     somebody notices. A missing policy is silence, and silence is
 *     indistinguishable from "there is nothing here yet".
 *   - It is one policy away from opening. Somebody adding a policy for an
 *     unrelated reason, or widening an existing one, turns a table nobody
 *     thought was reachable into a table that is.
 *
 * This was hit twice in one day: `capability_tokens` in 00015 and
 * `billing_webhook_events` in 00017. Both hold credential-adjacent state that
 * the application has no business reading, both looked closed, and both were
 * merely empty. Each was fixed with an explicit `revoke all ... from
 * kindlast_app`.
 *
 * So the rule is: if the application should not reach a table, revoke the
 * grant, do not rely on the absence of a policy. This asserts it over
 * `pg_class` and `pg_policy` rather than trusting that anybody remembered.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { connect, isStackReachable, SUPER_URL } from './helpers/db'

const reachable = await isStackReachable()

/**
 * Tables the application may address without a policy of its own.
 *
 * Empty, and it should stay that way. It held `goose_db_version` until ENT-243:
 * goose's migration bookkeeping carries no customer data, is not part of the
 * domain schema, and was swept into RLS only because 00002's loop covers every
 * table in `public`, so an exception looked like the honest answer. It was the
 * wrong one for exactly the reason this file argues everywhere else. `00029`
 * revoked the grant instead, and the table now fails closed at parse time like
 * everything else the application has no business naming.
 *
 * A new entry has to argue why revoking is worse than excepting, which is a
 * harder case than it sounds.
 */
const ALLOWED = new Set<string>()

let superuser: Client

beforeAll(async () => {
  if (!reachable) return
  superuser = await connect(SUPER_URL)
})

afterAll(async () => {
  if (!reachable) return
  await superuser.end()
})

/**
 * The same question, asked of one role.
 *
 * Parameterised because the trap is not specific to `kindlast_app`: any role
 * holding a blanket or default-privilege grant has it. `kindlast_agent`'s
 * grants were written explicitly in 00008 and 00015, and `kindlast_billing`'s
 * in 00017, so both should come back clean; asserting it is what stops a later
 * migration granting one of them something wholesale and nobody noticing that
 * the grant reaches further than the policies do.
 */
async function addressableWithoutPolicy(role: string): Promise<string[]> {
  const r = await superuser.query(
    `
      select c.relname
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and c.relkind = 'r'
        and c.relrowsecurity
        and has_table_privilege($1, c.oid, 'SELECT')
        and not exists (
          select 1 from pg_policy p
          where p.polrelid = c.oid
            and (p.polroles = '{0}' or $1::regrole = any(p.polroles))
        )
      order by 1
    `,
    [role],
  )
  return r.rows
    .map((row) => row.relname as string)
    .filter((name) => !ALLOWED.has(name))
}

describe.skipIf(!reachable)('tables the application can address', () => {
  it('either have a policy for it, or have the grant revoked', async () => {
    const r = await superuser.query(`
      select c.relname
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and c.relkind = 'r'
        and c.relrowsecurity
        and has_table_privilege('kindlast_app', c.oid, 'SELECT')
        and not exists (
          select 1 from pg_policy p
          where p.polrelid = c.oid
            -- polroles = '{0}' means the policy applies to PUBLIC, which
            -- includes kindlast_app.
            and (p.polroles = '{0}' or 'kindlast_app'::regrole = any(p.polroles))
        )
      order by 1
    `)

    const addressableButEmpty = r.rows
      .map((row) => row.relname as string)
      .filter((name) => !ALLOWED.has(name))

    expect(
      addressableButEmpty,
      'these tables are reachable by kindlast_app with no policy, so they are ' +
        'empty rather than private. Revoke the grant, or add the policy that ' +
        'was intended. See the header.',
    ).toEqual([])
  })
})

describe.skipIf(!reachable)('and the same holds for the system roles', () => {
  // 00008 and 00015 wrote kindlast_agent's grants one table at a time, and
  // 00017 did the same for kindlast_billing. Neither should hold a grant
  // reaching further than its policies.
  it.each(['kindlast_agent', 'kindlast_billing'])(
    '%s holds no grant without a matching policy',
    async (role) => {
      expect(
        await addressableWithoutPolicy(role),
        `${role} can address these tables with no policy of its own, so they ` +
          'are empty rather than closed to it',
      ).toEqual([])
    },
  )
})
