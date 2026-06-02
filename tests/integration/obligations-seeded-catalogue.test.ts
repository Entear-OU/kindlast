// @vitest-environment node
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { parseObligationsData } from '@/lib/corpus/obligations'

import { querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-157 — The obligations catalogue must be seeded by a migration, not by a
 * manual `pnpm ingest:obligations` run.
 *
 * Root cause of the bug: the curated corpus lived only in
 * `data/corpus/obligations.json` + a hand-run ingest script that no migration,
 * seed, or CI/deploy step ever invoked. So in any environment where nobody ran
 * the script — i.e. production for every real user — `public.obligations` was
 * empty, the Watcher's gap detector (`watcher_detect_gaps`) iterated zero
 * `requires`-bearing rows, and the feed came up empty for everyone.
 *
 * These assertions run against a freshly migrated DB with NO manual ingest:
 *
 *   1. Every slug in the curated corpus is present in `public.obligations`
 *      (proving the seed migration ran as part of `supabase start` / db reset).
 *   2. A representative SME profile with controls missing produces at least one
 *      `profile_gap` finding referencing a REAL corpus obligation — the
 *      end-to-end property the bug broke.
 *
 * The corpus JSON stays the single source of truth; the seed migration is
 * generated from it (see lib/corpus/obligations-seed.ts + the drift-guard unit
 * test), so this asserts the deployed snapshot matches the curated file.
 */

const supabaseRunning = await isLocalSupabaseReachable()

const CORPUS = parseObligationsData(
  JSON.parse(
    readFileSync(resolve(__dirname, '../../data/corpus/obligations.json'), 'utf8'),
  ),
)
const CORPUS_SLUGS = CORPUS.obligations.map((o) => o.slug)

describe.skipIf(!supabaseRunning)('seeded obligations catalogue (ENT-157)', () => {
  it('has every curated obligation present from migrations alone (no manual ingest)', async () => {
    const rows = await querySql<{ slug: string }>(
      `select slug from public.obligations where slug = any($1::text[])`,
      [CORPUS_SLUGS],
    )
    const present = new Set(rows.map((r) => r.slug))
    const missing = CORPUS_SLUGS.filter((s) => !present.has(s))
    expect(missing).toEqual([])
  })

  describe('end-to-end: the Watcher finds real gaps for a real profile', () => {
    let user: TestUser
    let profileId: string

    beforeAll(async () => {
      const admin = createServiceRoleClient()
      user = await signUpTestUser(admin)

      const { data: session } = await admin
        .from('onboarding_sessions')
        .insert({ user_id: user.id, status: 'completed' })
        .select('id')
        .single()

      // An SME that is a controller, uses AI, transfers data abroad, and has
      // none of the gated controls in place — so it must gap against several
      // real corpus obligations (ROPA, DPO, transfer safeguards, AI register).
      const { data: profile, error } = await admin
        .from('compliance_profiles')
        .insert({
          session_id: session!.id,
          user_id: user.id,
          industry: 'SaaS',
          ai_systems: ['internal ChatGPT'],
          has_dpo: 'no',
          has_ropa: 'no',
          transfers_outside_eu: 'yes',
          transfer_destinations: [],
          staff_count: 40,
          vendor_list: 'Stripe, AWS',
        })
        .select('id')
        .single()
      expect(error).toBeNull()
      profileId = profile!.id as string
    })

    afterAll(async () => {
      const admin = createServiceRoleClient()
      if (user?.id) await deleteTestUser(admin, user.id)
    })

    it('emits profile_gap findings tied to seeded corpus obligations', async () => {
      await querySql(`select public.run_watcher_for_profile($1::uuid)`, [profileId])

      const rows = await querySql<{ obligation_slug: string }>(
        `select obligation_slug from public.watcher_findings
         where profile_id = $1::uuid and kind = 'profile_gap'`,
        [profileId],
      )
      const slugs = rows.map((r) => r.obligation_slug)

      // The feed is no longer empty, and the gaps that matter trace back to
      // real curated obligations. We assert presence of corpus gaps rather
      // than "every finding is a corpus slug": the gap detector is global over
      // `public.obligations`, and parallel suites (ENT-56) inject `_test_*`
      // fixture obligations, so a strict universal check isn't hermetic.
      const corpus = new Set(CORPUS_SLUGS)
      const corpusGaps = slugs.filter((s) => corpus.has(s))
      expect(corpusGaps.length).toBeGreaterThan(0)
      // The DPO gap is the canonical example: controller without a DPO.
      expect(corpusGaps).toContain('gdpr-art-37-dpo-appointment')
    })
  })
})
