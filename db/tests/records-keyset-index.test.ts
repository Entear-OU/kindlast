/**
 * Every register RecordsService paginates has an index that supports the
 * ordering it paginates by (ENT-200, migration 00011).
 *
 * WHY THIS IS A TEST AND NOT A REVIEW COMMENT
 *
 * A missing index is not a wrong answer. The query returns exactly the right
 * rows either way, so no functional test can see it: `ai_systems` was indexed on
 * `(org_id)` alone from 00002 until 00011 and nothing failed, because nothing
 * listed AI systems. What changes is that Postgres reads the whole tenant's rows
 * and sorts them on every page, so the cost of page one grows with the size of
 * the register rather than the size of the page.
 *
 * That lands first on the customer with the largest register, which is the
 * customer who can least afford it to, and it lands long after the change that
 * caused it. Asserting the index structurally is the only way this gets caught
 * at the time.
 *
 * The sweep is deliberately over a list of tables rather than one: the next
 * register added to RecordsService should fail here if it repeats 00002's
 * asymmetry, rather than being noticed by whoever profiles it in a year.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { connect, isStackReachable, SUPER_URL } from './helpers/db'

const reachable = await isStackReachable()

/**
 * The registers RecordsService lists, and the column each one's keyset orders
 * by after `org_id`.
 *
 * `dsars` is ordered by `response_due_at` in the API because the only question
 * asked of that list is what runs out next, so it needs BOTH: `created_at` for
 * the record ordering and `response_due_at` for the deadline ordering. It is
 * listed once per index it must carry.
 */
const REGISTERS = [
  { table: 'processing_activities', second: 'created_at' },
  { table: 'ai_systems', second: 'created_at' },
  { table: 'dsars', second: 'created_at' },
  { table: 'dsars', second: 'response_due_at' },
] as const

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
 * The leading column names of every index on a table, in index order.
 *
 * Read from pg_index rather than by parsing pg_indexes.indexdef, because the
 * definition is a string and a test that greps a string passes for the wrong
 * reasons: `(org_id, created_at desc)` and `(created_at, org_id desc)` both
 * contain both names.
 *
 * `attname::text` is load-bearing. Without the cast the array comes back typed
 * `name[]`, which node-pg has no parser for, so every row arrives as the raw
 * literal string `'{org_id,created_at}'` instead of an array. Indexing that
 * yields `'{'`, every comparison below is false, and the redundant-index
 * assertion passes because a string is never length 1. Found by running this
 * file against the unmigrated database and reading which tests failed rather
 * than counting them: it reported the two real gaps AND two tables whose index
 * was present all along.
 */
async function indexColumns(
  client: Client,
  table: string,
): Promise<string[][]> {
  const r = await client.query(
    `
    select i.relname as index_name,
           array(
             select a.attname::text
             from unnest(ix.indkey::int[]) with ordinality as k(attnum, ord)
             join pg_attribute a
               on a.attrelid = t.oid and a.attnum = k.attnum
             order by k.ord
           ) as columns
    from pg_index ix
    join pg_class t on t.oid = ix.indrelid
    join pg_class i on i.oid = ix.indexrelid
    join pg_namespace n on n.oid = t.relnamespace
    where n.nspname = 'public' and t.relname = $1
  `,
    [table],
  )
  return r.rows.map((row) => row.columns as string[])
}

describe.skipIf(!reachable)('keyset indexes on the record registers', () => {
  for (const { table, second } of REGISTERS) {
    it(`${table} has an index leading with (org_id, ${second})`, async () => {
      const indexes = await indexColumns(superuser, table)

      // Leading two columns, in order. A composite that merely CONTAINS both is
      // not enough: Postgres can only use the prefix, so (created_at, org_id)
      // does not serve a per-tenant ordered scan.
      const supported = indexes.some(
        (columns) => columns[0] === 'org_id' && columns[1] === second,
      )

      expect(
        supported,
        `${table} has no index leading with (org_id, ${second}). ` +
          `Indexes present: ${JSON.stringify(indexes)}`,
      ).toBe(true)
    })
  }

  it('ai_systems no longer carries the redundant org-only index', async () => {
    // 00011 drops it because it became a strict prefix of the composite, so it
    // can answer no read the composite cannot while still costing a write on
    // every insert and update. Asserted rather than assumed: "I dropped it in
    // the migration" and "it is gone from the database" are different claims,
    // and a `drop index if exists` naming the wrong index makes both look true.
    const indexes = await indexColumns(superuser, 'ai_systems')
    const orgOnly = indexes.filter(
      (columns) => columns.length === 1 && columns[0] === 'org_id',
    )
    expect(orgOnly).toHaveLength(0)
  })
})
