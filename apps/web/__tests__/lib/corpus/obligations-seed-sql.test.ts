import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { parseObligationsData, type ObligationsData } from '@/lib/corpus/obligations'
import { buildObligationsSeedMigration } from '@/lib/corpus/obligations-seed'

/**
 * Unit coverage for the obligations seed-SQL generator (ENT-157) and the
 * drift guard that keeps the committed migration in sync with the corpus.
 */

const CORPUS_PATH = resolve(__dirname, '../../../../../data/corpus/obligations.json')
const MIGRATION_PATH = resolve(
  __dirname,
  '../../../../../supabase/migrations/20260602120000_seed_obligations_catalogue.sql',
)

const corpus: ObligationsData = parseObligationsData(
  JSON.parse(readFileSync(CORPUS_PATH, 'utf8')),
)

describe('buildObligationsSeedMigration', () => {
  it('emits one VALUES tuple per obligation', () => {
    const sql = buildObligationsSeedMigration(corpus)
    // Each obligation row is rendered as `('<slug>',` at the start of a tuple.
    for (const o of corpus.obligations) {
      expect(sql).toContain(`('${o.slug}',`)
    }
  })

  it('is idempotent by design — upserts on the slug natural key', () => {
    const sql = buildObligationsSeedMigration(corpus)
    expect(sql).toContain('on conflict (slug) do update set')
    // Every non-slug column is refreshed from the incoming row.
    expect(sql).toContain('summary = excluded.summary')
    expect(sql).toContain('applies_when = excluded.applies_when')
    expect(sql).toContain('topic_tags = excluded.topic_tags')
    // The conflict target itself is never reassigned.
    expect(sql).not.toContain('slug = excluded.slug')
  })

  it('preserves the requires-bearing applies_when rules the Watcher depends on', () => {
    const sql = buildObligationsSeedMigration(corpus)
    // ENT-157's whole point: these rules must reach the DB intact.
    expect(sql).toContain('"requires":["ropa"]')
    expect(sql).toContain('"requires":["dpo"]')
    expect(sql).toContain('"requires":["transfer_safeguards"]')
    expect(sql).toContain('"requires":["ai_register"]')
  })

  it('escapes single quotes in text so apostrophes do not break the SQL', () => {
    const data: ObligationsData = {
      obligations: [
        {
          slug: 'fixture-apostrophe',
          title: "Controller's record",
          summary:
            "A fixture summary containing an apostrophe in the controller's name and elsewhere, long enough to read naturally while we assert the single-quote doubling behaviour of the generator.",
          citation: { kind: 'article', celex: '32016R0679', articleNumber: 30 },
          appliesWhen: { role: 'controller' },
          severity: 'medium',
          topicTags: ['fixture'],
        },
      ],
    }
    const sql = buildObligationsSeedMigration(data)
    expect(sql).toContain("'Controller''s record'")
    expect(sql).not.toContain("'Controller's record'")
  })

  it('renders text[] topic_tags and ::jsonb applies_when as typed literals', () => {
    const sql = buildObligationsSeedMigration(corpus)
    expect(sql).toMatch(/array\[[^\]]*\]::text\[\]/)
    expect(sql).toMatch(/'\{[^']*\}'::jsonb/)
  })

  it('renders deterministically — same corpus in, byte-identical SQL out', () => {
    expect(buildObligationsSeedMigration(corpus)).toBe(
      buildObligationsSeedMigration(corpus),
    )
  })
})

describe('seed migration drift guard', () => {
  it('the committed migration matches the corpus (regenerate to fix)', () => {
    const committed = readFileSync(MIGRATION_PATH, 'utf8')
    expect(committed).toBe(buildObligationsSeedMigration(corpus))
  })
})
