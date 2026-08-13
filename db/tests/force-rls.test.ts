/**
 * FORCE ROW LEVEL SECURITY, asserted by a query over pg_class rather than by
 * convention (ENT-192 acceptance criterion).
 *
 * The trap (core-api-surface §14.1): a table owner bypasses RLS unless the
 * table sets FORCE ROW LEVEL SECURITY. All tables in `public` are owned by
 * kindlast_migrator, so without FORCE, anything connecting as the migrator
 * reads across tenants silently. The sweep is structural: every table carrying
 * an org_id column is a tenant table by definition, and every one of them must
 * have both relrowsecurity and relforcerowsecurity set. A new tenant table
 * added without FORCE fails this test, not a code review.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { connect, isStackReachable, SUPER_URL } from './helpers/db'

const reachable = await isStackReachable()

let superuser: Client

beforeAll(async () => {
  if (!reachable) return
  superuser = await connect(SUPER_URL)
})

afterAll(async () => {
  if (!reachable) return
  await superuser.end()
})

describe.skipIf(!reachable)('FORCE ROW LEVEL SECURITY on every tenant table', () => {
  it('every table with an org_id column has RLS enabled AND forced', async () => {
    const r = await superuser.query(`
      select c.relname as table_name,
             c.relrowsecurity as enabled,
             c.relforcerowsecurity as forced
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and c.relkind = 'r'
        and exists (
          select 1 from pg_attribute a
          where a.attrelid = c.oid and a.attname = 'org_id' and not a.attisdropped
        )
      order by c.relname
    `)
    // If this is empty the org model has not landed, which is its own failure.
    expect(r.rows.length).toBeGreaterThanOrEqual(15)
    const broken = r.rows.filter((row) => !row.enabled || !row.forced)
    expect(
      broken.map((b) => b.table_name),
      'tenant tables missing FORCE ROW LEVEL SECURITY',
    ).toEqual([])
  })

  it('organisations and memberships themselves are RLS-forced', async () => {
    const r = await superuser.query(`
      select relname, relrowsecurity, relforcerowsecurity
      from pg_class
      where relnamespace = 'public'::regnamespace
        and relname in ('organisations', 'memberships')
      order by relname
    `)
    expect(r.rows).toHaveLength(2)
    for (const row of r.rows) {
      expect(row.relrowsecurity, `${row.relname} rowsecurity`).toBe(true)
      expect(row.relforcerowsecurity, `${row.relname} force rowsecurity`).toBe(true)
    }
  })

  /**
   * The sweep above keys on org_id, which is the right definition of a tenant
   * table and the wrong definition of "everything that needs a policy".
   *
   * ENT-196 added `user_identities`, which holds an issuer, a subject and an
   * email, and deliberately carries no org_id: identity is not tenant-scoped,
   * because one human is the same person across several organisations. It is
   * personal data with no org_id, so every assertion in this file would have
   * stayed green had it shipped with no RLS at all.
   *
   * AGENTS.md already states the rule as "every table in public has RLS
   * enabled and forced". This asserts the rule as written rather than a
   * weaker version of it. There is deliberately no allow-list: a table that
   * genuinely needs no policy can have one that is simply permissive, and
   * writing that down is a decision somebody made rather than an omission
   * nobody noticed.
   */
  it('every table in public has RLS enabled and forced, org_id or not', async () => {
    const r = await superuser.query(`
      select relname
      from pg_class
      where relnamespace = 'public'::regnamespace
        and relkind = 'r'
        and not (relrowsecurity and relforcerowsecurity)
      order by relname
    `)
    expect(
      r.rows.map((x) => x.relname),
      'tables in public without RLS enabled AND forced',
    ).toEqual([])
  })

  it('the tables ENT-196 added for provisioning are policed', async () => {
    const r = await superuser.query(`
      select relname, relrowsecurity, relforcerowsecurity
      from pg_class
      where relnamespace = 'public'::regnamespace
        and relname in ('user_identities', 'invitations')
      order by relname
    `)
    // Named explicitly, so deleting them is a decision rather than a silent
    // reduction in what the sweep above happens to cover.
    expect(r.rows).toHaveLength(2)
    for (const row of r.rows) {
      expect(row.relrowsecurity, `${row.relname} rowsecurity`).toBe(true)
      expect(row.relforcerowsecurity, `${row.relname} force rowsecurity`).toBe(true)
    }
  })

  it('every RLS-enabled table in public is also forced (no owner loophole anywhere)', async () => {
    const r = await superuser.query(`
      select relname from pg_class
      where relnamespace = 'public'::regnamespace
        and relkind = 'r'
        and relrowsecurity
        and not relforcerowsecurity
      order by relname
    `)
    expect(r.rows.map((x) => x.relname)).toEqual([])
  })
})
