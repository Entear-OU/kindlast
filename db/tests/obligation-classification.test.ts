/**
 * Which obligations make the Executor do something (ENT-203, closing ENT-165).
 *
 * 00009 applied the product owner's ruling of 2026-08-15. This suite pins it,
 * and the test that earns its place is the last one: nothing may map to
 * `create_dsar`.
 *
 * The reasoning is worth repeating where someone changing the mapping will see
 * it. `create_ropa` and `create_ai_system` create a record the customer owns
 * and edits. `create_dsar` creates a data subject request with a 30-day
 * statutory clock, and `executor_create_dsar_on_approval` starts that clock at
 * approval with a subject taken from a payload a profile-gap finding does not
 * carry. Mapping an obligation to it would invent a legal deadline for a
 * request nobody made, and show it to the customer as something they are
 * running out of time on (ENT-224).
 *
 * These assertions are deliberately exact rather than "at least". A mapping
 * that grows by accident is the failure mode worth catching: the Executor
 * changes a customer's compliance record, so an obligation joining that set
 * should be a decision somebody made and a test somebody edited.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { connect, isStackReachable, MIGRATOR_URL } from './helpers/db'

const reachable = await isStackReachable()

let migrator: Client

/** The ruling, restated here so the test does not read the migration. */
const CLASSIFIED: Record<string, string> = {
  'gdpr-art-30-ropa': 'create_ropa',
  'ai-act-annex-iii-high-risk-systems': 'create_ai_system',
  'ai-act-art-26-deployer-obligations': 'create_ai_system',
}

/** Considered and left at review, because each concerns a field on a record
 *  rather than the record itself. They move only if an update action lands. */
const DELIBERATELY_REVIEW = [
  'gdpr-arts-12-22-data-subject-rights',
  'gdpr-art-6-lawful-basis',
  'gdpr-chapter-v-international-transfers',
  'gdpr-art-28-processor-contracts',
  'ai-act-art-50-transparency',
]

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
})

afterAll(async () => {
  if (!reachable) return
  await migrator.end()
})

describe.skipIf(!reachable)('the obligation classification', () => {
  it.each(Object.entries(CLASSIFIED))(
    '%s makes approving create a %s',
    async (slug, action) => {
      const r = await migrator.query(
        `select action_type from obligations where slug = $1`,
        [slug],
      )
      expect(r.rows).toHaveLength(1)
      expect(r.rows[0].action_type).toBe(action)
    },
  )

  it.each(DELIBERATELY_REVIEW)('%s stays a review', async (slug) => {
    const r = await migrator.query(
      `select action_type from obligations where slug = $1`,
      [slug],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].action_type).toBe('review')
  })

  // Exact, not "at least". An obligation joining the Executor's set changes
  // what approving does to a customer's compliance record, so it should require
  // editing this list rather than slipping in.
  it('classifies exactly three obligations and no more', async () => {
    const r = await migrator.query(
      `select slug from obligations
        where action_type <> 'review'
          and slug not like 'rpc-fixture-%'
        order by slug`,
    )
    expect(r.rows.map((row) => row.slug).sort()).toEqual(
      Object.keys(CLASSIFIED).sort(),
    )
  })

  // The one that matters most. See the header: mapping anything here invents a
  // statutory deadline for a request nobody made.
  it('maps nothing to create_dsar', async () => {
    const r = await migrator.query(
      `select slug from obligations where action_type = 'create_dsar'`,
    )
    expect(r.rows).toEqual([])
  })
})
