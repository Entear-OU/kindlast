/**
 * The corpus is the law, and only one role may write it (ENT-207).
 *
 * # WHY THIS SUITE IS SHAPED DIFFERENTLY FROM THE OTHERS
 *
 * Every other table in this schema is tenant data, and the property under test
 * is that one organisation cannot see another's. The corpus is the opposite:
 * it is the same regulation for every customer, it has no `org_id`, and the
 * property under test is that everybody CAN read it and almost nobody can
 * write it.
 *
 * ENT-207 says in terms not to add an `org_id` to these tables to make them
 * look like the rest of the schema. So the first test here asserts their
 * absence, because a well-meaning future migration adding one would break
 * cross-tenant reads while looking like a consistency fix.
 *
 * # WHY THE WRITE SIDE MATTERS MORE THAN IT LOOKS
 *
 * AGENTS.md opens by saying that anything fabricating a citation is worse than
 * nothing, because the product's value is that a human can check the claim
 * against the law. That only holds if the thing serving the claim cannot edit
 * the law. `kindlast_app` answers browser requests and `kindlast_agent` can
 * invent a finding; either of them holding corpus writes would make a citation
 * something the system could manufacture end to end.
 *
 * Before 00018, `kindlast_app` held `insert, update, delete` on all ten tables
 * and simply had no policy to use them with, which is the addressable-but-empty
 * trap in its protective direction: correct behaviour resting on an absence
 * rather than on a grant.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  roleUrl,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const INGEST_URL = roleUrl('ingest')
const AGENT_URL = roleUrl('agent')

/** The ten tables that hold the law rather than a customer's data. */
const CORPUS_TABLES = [
  'regulatory_documents',
  'regulatory_articles',
  'regulatory_article_paragraphs',
  'regulatory_article_recitals',
  'regulatory_recitals',
  'regulatory_annexes',
  'regulatory_annex_items',
  'regulatory_guidelines',
  'regulatory_enforcement_decisions',
  'obligations',
]

let migrator: Client
let app: Client
let ingest: Client
let agent: Client

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  ingest = await connect(INGEST_URL)
  agent = await connect(AGENT_URL)
})

afterAll(async () => {
  await migrator?.end()
  await app?.end()
  await ingest?.end()
  await agent?.end()
})

describe.skipIf(!reachable)('the corpus is not tenant data', () => {
  it('carries no org_id on any table', async () => {
    // Asserted rather than assumed, because adding one would look like a
    // consistency fix and would break every cross-tenant read.
    const { rows } = await migrator.query(
      `select table_name from information_schema.columns
        where table_schema = 'public'
          and column_name = 'org_id'
          and table_name = any($1::text[])
        order by table_name`,
      [CORPUS_TABLES],
    )

    expect(rows.map((r) => r.table_name)).toEqual([])
  })

  it('reads the same rows whichever organisation the caller is in', async () => {
    const celex = `TEST-${process.pid}-${Date.now()}`
    await migrator.query(
      `insert into regulatory_documents
         (celex_number, title, short_title, version_date, official_url)
       values ($1, 'Test document', 'Test', '2026-01-01', 'https://example.test/doc')`,
      [celex],
    )

    try {
      // Two different organisations in the GUCs. A tenant table would answer
      // differently; the corpus must not.
      const seen: number[] = []
      for (const org of [
        '11111111-1111-4111-8111-111111111111',
        '22222222-2222-4222-8222-222222222222',
      ]) {
        await app.query("select set_config('app.current_org_id', $1, false)", [
          org,
        ])
        await app.query("select set_config('app.current_user_id', $1, false)", [
          '33333333-3333-4333-8333-333333333333',
        ])
        const { rows } = await app.query(
          'select count(*)::int as n from regulatory_documents where celex_number = $1',
          [celex],
        )
        seen.push(rows[0].n)
      }

      expect(seen).toEqual([1, 1])
    } finally {
      await migrator.query(
        'delete from regulatory_documents where celex_number = $1',
        [celex],
      )
    }
  })

  it('is readable with no tenancy GUCs set at all', async () => {
    // The public-read policies are `using (true)` and must not depend on the
    // GUCs. A corpus that needed an organisation to be readable would make the
    // law itself tenant data by accident.
    const fresh = await connect(APP_URL)
    try {
      const { rows } = await fresh.query(
        'select count(*)::int as n from obligations',
      )
      expect(rows[0].n).toBeGreaterThan(0)
    } finally {
      await fresh.end()
    }
  })
})

describe.skipIf(!reachable)('only the ingest role writes the corpus', () => {
  it('does not let the request-handling role write any corpus table', async () => {
    // The GUCs are set, so this is not a tenancy failure: `kindlast_app` simply
    // has no grant. Before 00018 it had one and was stopped only by a missing
    // policy, which is correct behaviour resting on an absence.
    await app.query("select set_config('app.current_org_id', $1, false)", [
      '11111111-1111-4111-8111-111111111111',
    ])
    await app.query("select set_config('app.current_user_id', $1, false)", [
      '33333333-3333-4333-8333-333333333333',
    ])

    for (const table of CORPUS_TABLES) {
      await app.query('begin')
      let refused = false
      try {
        // A no-op update rather than an insert: it needs no valid column
        // values, so what it proves is the privilege rather than a constraint.
        await app.query(`update ${table} set created_at = created_at`)
      } catch (error) {
        // 42501 is insufficient_privilege. Asserting the code rather than just
        // "it threw", because a constraint violation would also throw and would
        // prove nothing about who may write.
        refused = (error as { code?: string }).code === '42501'
      }
      await app.query('rollback')

      expect(refused, `${table} was writable by kindlast_app`).toBe(true)
    }
  })

  it('does not let the agent write the corpus it reads', async () => {
    // The sharpest version of the rule. The agent can invent a finding; if it
    // could also author the obligation that finding cites, the product would
    // contain a machine that manufactures a citation end to end.
    let refused = false
    await agent.query('begin')
    try {
      await agent.query('update obligations set updated_at = updated_at')
    } catch (error) {
      refused = (error as { code?: string }).code === '42501'
    }
    await agent.query('rollback')

    expect(refused).toBe(true)
  })

  it('lets the ingest role insert and update, and re-ingest without duplicating', async () => {
    const celex = `TEST-${process.pid}-${Date.now()}`

    try {
      // Insert, then the same natural key again with different content. The
      // second must update rather than duplicate: that IS idempotence.
      for (const title of ['First title', 'Second title']) {
        await ingest.query(
          `insert into regulatory_documents
             (celex_number, title, short_title, version_date, official_url)
           values ($1, $2, 'Test', '2026-01-01', 'https://example.test/doc')
           on conflict (celex_number) do update
             set title = excluded.title, updated_at = now()`,
          [celex, title],
        )
      }

      const { rows } = await ingest.query(
        'select title from regulatory_documents where celex_number = $1',
        [celex],
      )
      expect(rows).toHaveLength(1)
      expect(rows[0].title).toBe('Second title')
    } finally {
      await migrator.query(
        'delete from regulatory_documents where celex_number = $1',
        [celex],
      )
    }
  })

  it('does not let the ingest role delete, so a citation cannot dangle', async () => {
    // A finding cites an obligation and an obligation cites an article.
    // Deleting either out from under a stored finding turns a claim a customer
    // could check into a dangling reference, retroactively, in a record they
    // may already have shown a regulator.
    let refused = false
    await ingest.query('begin')
    try {
      await ingest.query('delete from obligations')
    } catch (error) {
      refused = (error as { code?: string }).code === '42501'
    }
    await ingest.query('rollback')

    expect(refused).toBe(true)
  })
})

describe.skipIf(!reachable)('the ingest role reaches nothing else', () => {
  /**
   * Tables the corpus writer has no business touching. Not an exhaustive list
   * of the schema: these are the four that would matter most if its credential
   * leaked, one from each category the product protects.
   */
  const FORBIDDEN = ['findings', 'memberships', 'organisations', 'audit_log']

  it('holds no grant on tenant tables', async () => {
    const { rows } = await migrator.query(
      `select table_name, privilege_type
         from information_schema.role_table_grants
        where grantee = 'kindlast_ingest'
          and table_schema = 'public'
          and table_name = any($1::text[])`,
      [FORBIDDEN],
    )

    expect(rows).toEqual([])
  })

  it('is refused when it tries anyway', async () => {
    // The grant table above is the structural assertion. This is the behavioural
    // one, because a grant reaching a role through PUBLIC or through a role
    // membership would not show up in that query.
    for (const table of FORBIDDEN) {
      let refused = false
      await ingest.query('begin')
      try {
        await ingest.query(`select count(*) from ${table}`)
      } catch (error) {
        refused = (error as { code?: string }).code === '42501'
      }
      await ingest.query('rollback')

      expect(refused, `kindlast_ingest could read ${table}`).toBe(true)
    }
  })

  it('holds grants on the corpus and nothing besides', async () => {
    // The whole surface, enumerated. A future migration widening this role
    // fails here rather than being noticed by somebody reading a diff.
    const { rows } = await migrator.query(
      `select distinct table_name
         from information_schema.role_table_grants
        where grantee = 'kindlast_ingest' and table_schema = 'public'
        order by table_name`,
    )

    expect(rows.map((r) => r.table_name).sort()).toEqual(
      [...CORPUS_TABLES].sort(),
    )
  })
})
