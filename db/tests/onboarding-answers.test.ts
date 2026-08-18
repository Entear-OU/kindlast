/**
 * The onboarding transcript's boundary (ENT-212, 00026).
 *
 * Two properties, and they are not the same kind of thing.
 *
 * The first is tenancy, and it is inherited rather than new: both onboarding
 * tables have carried `org_id`, FORCE ROW LEVEL SECURITY and the two-GUC
 * policies since 00002. 00026 adds columns to one of them, and the reason to
 * assert isolation anyway is that "the columns you added are covered by the
 * policies that were already there" is exactly the sort of claim that is true
 * until somebody adds the next table by copying this one.
 *
 * The second is the one 00026 introduces, and it is the design's load-bearing
 * constraint. Every value in a customer's compliance profile is supposed to
 * have come from a person's typed answer. `onboarding_messages_value_is_an_answer`
 * is what makes that structural: an assistant turn cannot carry a value, so the
 * product cannot answer its own question, whatever the handler writing the row
 * believes it is doing.
 *
 * PROVEN ABLE TO FAIL. Dropping the constraint from 00026 turns "the product
 * cannot answer its own question" red on its own and leaves the isolation
 * checks green, which is the shape that says these test the boundary rather
 * than the plumbing.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  setTenant,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

let migrator: Client
let app: Client

const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()
const bob = randomUUID()

let sessionA = ''
let sessionB = ''

async function seedOrg(
  org: string,
  label: string,
  member: string,
): Promise<string> {
  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `${label} ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, member],
  )
  const session = await migrator.query(
    `insert into onboarding_sessions (org_id, created_by) values ($1, $2) returning id`,
    [org, member],
  )
  return session.rows[0].id as string
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  sessionA = await seedOrg(orgA, 'Onboarding A', ada)
  sessionB = await seedOrg(orgB, 'Onboarding B', bob)
  await setTenant(app, orgA, ada)
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    orgA,
    orgB,
  ])
  await Promise.all([migrator.end(), app.end()])
})

describe.skipIf(!reachable)(
  'an answer belongs to the person who gave it',
  () => {
    it('records what was typed and what it was taken to mean', async () => {
      const r = await app.query(
        `insert into onboarding_messages
         (org_id, session_id, created_by, role, content, ordering, fact_key, fact_value)
       values ($1, $2, $3, 'user', 'Ireland, Spain', 0, 'eu_jurisdictions', $4::jsonb)
       returning fact_key, fact_value`,
        [orgA, sessionA, ada, JSON.stringify(['Ireland', 'Spain'])],
      )
      expect(r.rows[0].fact_key).toBe('eu_jurisdictions')
      expect(r.rows[0].fact_value).toEqual(['Ireland', 'Spain'])
    })

    it('lets a question name the fact it asks about, with no value', async () => {
      const r = await app.query(
        `insert into onboarding_messages
         (org_id, session_id, created_by, role, content, ordering, fact_key)
       values ($1, $2, $3, 'assistant', 'Have you appointed a DPO?', 1, 'has_dpo')
       returning fact_value`,
        [orgA, sessionA, ada],
      )
      // Null rather than a placeholder. A question with a value would be the
      // product having answered itself, which the next test refuses outright.
      expect(r.rows[0].fact_value).toBeNull()
    })

    it('refuses a value with no question attached to it', async () => {
      await expect(
        app.query(
          `insert into onboarding_messages
           (org_id, session_id, created_by, role, content, ordering, fact_value)
         values ($1, $2, $3, 'user', 'Ireland', 2, '"Ireland"'::jsonb)`,
          [orgA, sessionA, ada],
        ),
      ).rejects.toThrow(/onboarding_messages_value_is_an_answer/i)
    })

    it('refuses to let the product answer its own question', async () => {
      // THE CONSTRAINT THIS MIGRATION EXISTS FOR. The profile decides which
      // obligations apply to an organisation, so a value that came from anywhere
      // but a person's typed answer produces findings nobody gave grounds for.
      // Enforced here rather than in the handler, because the handler is the
      // thing most likely to be rewritten by somebody adding a model to the loop.
      await expect(
        app.query(
          `insert into onboarding_messages
           (org_id, session_id, created_by, role, content, ordering, fact_key, fact_value)
         values ($1, $2, $3, 'assistant', 'Probably Ireland', 3,
                 'eu_jurisdictions', $4::jsonb)`,
          [orgA, sessionA, ada, JSON.stringify(['Ireland'])],
        ),
      ).rejects.toThrow(/onboarding_messages_value_is_an_answer/i)
    })
  },
)

describe.skipIf(!reachable)(
  'one organisation cannot read another interview',
  () => {
    it('sees none of the other organisation transcript, by id', async () => {
      await migrator.query(
        `insert into onboarding_messages
         (org_id, session_id, created_by, role, content, ordering, fact_key, fact_value)
       values ($1, $2, $3, 'user', 'we sell bread', 0, 'industry', '"we sell bread"'::jsonb)`,
        [orgB, sessionB, bob],
      )

      // Addressed by the session id, which is the read the store makes. Guessing
      // the id must not help, which is the whole difference between a filter and
      // a boundary.
      const r = await app.query(
        `select count(*)::int as n from onboarding_messages where session_id = $1`,
        [sessionB],
      )
      expect(r.rows[0].n).toBe(0)
    })

    it('cannot write an answer into another organisation session', async () => {
      // Refused by the insert policy's with-check rather than ignored. A row
      // written here would be an answer in somebody else's compliance profile.
      await expect(
        app.query(
          `insert into onboarding_messages
           (org_id, session_id, created_by, role, content, ordering, fact_key, fact_value)
         values ($1, $2, $3, 'user', 'we sell nothing', 1, 'industry', '"we sell nothing"'::jsonb)`,
          [orgB, sessionB, ada],
        ),
      ).rejects.toThrow(/row-level security/i)
    })

    it('sees the other organisation rows once it is the other organisation', async () => {
      // The control that makes the two checks above mean something. Without it,
      // an empty result could equally mean the fixture never landed.
      const other = await connect(APP_URL)
      try {
        await setTenant(other, orgB, bob)
        const r = await other.query(
          `select count(*)::int as n from onboarding_messages where session_id = $1`,
          [sessionB],
        )
        expect(r.rows[0].n).toBeGreaterThan(0)
      } finally {
        await other.end()
      }
    })
  },
)
