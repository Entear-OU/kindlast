// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { querySql } from './helpers/db-fixture'
import {
  createAnonClient,
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser } from './helpers/test-user'

/**
 * Table privileges for the Supabase roles (ENT-159).
 *
 * RLS only filters rows *within* privileges a role already holds — a policy on a
 * table the role cannot SELECT never runs, and Postgres raises "permission
 * denied for table" first. Every product table here has RLS enabled and
 * policies defined, but no migration ever issued a GRANT, so on a fresh stack a
 * logged-in user hit an error boundary on 6 of 8 authed routes and the agent
 * pipeline had no access at all.
 *
 * The privilege model is deliberately permissive-grants + RLS-as-the-gate,
 * matching how hosted Supabase bootstraps a project and how the rest of the
 * suite asserts behaviour: a denied read or write returns a silent empty result,
 * not a 42501. Narrowing grants per policy would turn those no-ops into raw
 * Postgres errors in the UI. Security therefore rests on RLS, which these tests
 * re-verify at the end.
 */

const supabaseRunning = await isLocalSupabaseReachable()

const ROLES = ['anon', 'authenticated', 'service_role'] as const
const DML = ['SELECT', 'INSERT', 'UPDATE', 'DELETE'] as const

/**
 * Fixture tables belong to other suites running in parallel and can vanish
 * mid-query, so every lookup is oid-based and skips them.
 */
const PRODUCT_TABLES = /* sql */ `
  select c.oid, c.relname
    from pg_class c
   where c.relnamespace = 'public'::regnamespace
     and c.relkind = 'r'
     and c.relname not like '\\_test\\_%'
`

describe.skipIf(!supabaseRunning)('table privileges (ENT-159)', () => {
  it.each(ROLES)('%s holds full DML on every product table', async (role) => {
    const gaps = await querySql<{ relname: string; missing: string }>(
      /* sql */ `
        with t as (${PRODUCT_TABLES})
        select t.relname, p.privilege as missing
          from t
          cross join unnest($1::text[]) as p(privilege)
         where not has_table_privilege($2, t.oid, p.privilege)
         order by t.relname, p.privilege
      `,
      [[...DML], role],
    )

    expect(
      gaps.map((g) => `${g.relname}:${g.missing}`),
      `${role} is missing grants — RLS policies on these tables are dead code`,
    ).toEqual([])
  })

  it('finds at least the full product schema (guards against an empty query)', async () => {
    const rows = await querySql<{ relname: string }>(PRODUCT_TABLES)
    // Sanity check: if the lookup silently returned nothing, the assertions
    // above would pass vacuously.
    expect(rows.length).toBeGreaterThanOrEqual(26)
  })

  describe('RLS still gates access once the grants are in place', () => {
    let userA: { id: string; email: string; password: string }
    let userB: { id: string; email: string; password: string }

    beforeAll(async () => {
      const admin = createServiceRoleClient()
      userA = await signUpTestUser(admin)
      userB = await signUpTestUser(admin)

      const { error } = await admin
        .from('onboarding_sessions')
        .insert([{ user_id: userA.id }, { user_id: userB.id }])
      if (error) throw error
    })

    afterAll(async () => {
      const admin = createServiceRoleClient()
      if (userA?.id) await deleteTestUser(admin, userA.id)
      if (userB?.id) await deleteTestUser(admin, userB.id)
    })

    it('a logged-in user reads their own row', async () => {
      const client = await createUserClient(userA.email, userA.password)
      const { data, error } = await client
        .from('onboarding_sessions')
        .select('id, user_id')

      expect(error).toBeNull()
      expect(data).toHaveLength(1)
      expect(data![0]).toMatchObject({ user_id: userA.id })
    })

    it("a logged-in user cannot see another user's row", async () => {
      const client = await createUserClient(userA.email, userA.password)
      const { data, error } = await client
        .from('onboarding_sessions')
        .select('id')
        .eq('user_id', userB.id)

      // The grant must not weaken isolation: RLS hides the row silently.
      expect(error).toBeNull()
      expect(data).toEqual([])
    })

    it('anon cannot read user-owned tables despite holding the grant', async () => {
      const anon = createAnonClient()
      const { data, error } = await anon.from('onboarding_sessions').select('id')

      expect(error).toBeNull()
      expect(data).toEqual([])
    })

    it('billing_webhook_events stays unreachable — RLS enabled, zero policies', async () => {
      const policies = await querySql<{ count: string }>(
        `select count(*) as count from pg_policies where schemaname='public' and tablename='billing_webhook_events'`,
      )
      expect(Number(policies[0]!.count)).toBe(0)

      const client = await createUserClient(userA.email, userA.password)
      const { data, error } = await client.from('billing_webhook_events').select('*')

      expect(error).toBeNull()
      expect(data).toEqual([])
    })
  })
})
