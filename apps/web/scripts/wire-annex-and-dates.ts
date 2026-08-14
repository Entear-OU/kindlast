#!/usr/bin/env tsx
/**
 * One-shot enrichment for ENT-96. Reads the standalone Annex III snapshot
 * the data agent produced (`data/corpus/eu-ai-act-annex-iii.json`) and
 * folds it into the main AI Act file (`data/corpus/eu-ai-act.json`) as the
 * `annexes` array. Also stamps:
 *
 *   * Article 4 (AI literacy) with `effectiveDate: "2025-02-02"` — already
 *     in force since the AI Act's staged calendar (Article 113, point (a)).
 *   * Annex III with `effectiveDate: "2026-08-02"` — the Article 6(2) +
 *     Annex III high-risk regime kicks in then. Assumes no Digital
 *     Omnibus deferral per PRD §3.
 *
 * Idempotent — re-running produces byte-identical output.
 *
 * After this lands once, `data/corpus/eu-ai-act-annex-iii.json` is
 * deleted (its content lives in the main file); the script then becomes
 * a no-op on the merged file.
 *
 * Usage:
 *
 *   bun run wire:ai-act-annex
 */

import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { cwd, exit } from 'node:process'

const AI_ACT_PATH = '../../data/corpus/eu-ai-act.json'
const ANNEX_III_PATH = '../../data/corpus/eu-ai-act-annex-iii.json'

const ARTICLE_4_EFFECTIVE_DATE = '2025-02-02'

// Note: this script is historical — it ran once to merge a standalone
// annex JSON into the AI Act snapshot and then deleted the source file.
// The `summary` field reflects the progressive-disclosure shape adopted
// in the ENT-32 architecture update (2026-05-27); if the script is ever
// re-run, the standalone annex JSON must use `summary` (≥100 chars), not
// the legacy `body` field.
type Annex = {
  label: string
  heading: string
  summary: string
  effectiveDate?: string
  items: Array<{
    label: string
    heading?: string
    summary: string
    ordering: number
    effectiveDate?: string
  }>
}

type Article = {
  articleNumber: number
  heading: string
  summary: string
  paragraphs?: Array<{ label: string; summary: string; ordering: number }>
  effectiveDate?: string
}

type Snapshot = {
  document: unknown
  articles: Article[]
  recitals: unknown[]
  annexes?: Annex[]
}

function main(): void {
  const aiActAbs = resolve(cwd(), AI_ACT_PATH)
  const annexAbs = resolve(cwd(), ANNEX_III_PATH)

  console.log(`wire-annex-and-dates: reading ${aiActAbs}`)
  const snapshot = JSON.parse(readFileSync(aiActAbs, 'utf8')) as Snapshot

  // ─── Article 4 effective date ────────────────────────────────────────────
  const article4 = snapshot.articles.find((a) => a.articleNumber === 4)
  if (!article4) {
    throw new Error('wire-annex-and-dates: Article 4 not found in snapshot')
  }
  article4.effectiveDate = ARTICLE_4_EFFECTIVE_DATE
  console.log(`  Article 4: effectiveDate = ${ARTICLE_4_EFFECTIVE_DATE}`)

  // ─── Annex III merge ─────────────────────────────────────────────────────
  if (existsSync(annexAbs)) {
    console.log(`wire-annex-and-dates: merging ${annexAbs}`)
    const annexFile = JSON.parse(readFileSync(annexAbs, 'utf8')) as {
      annex: Omit<Annex, 'items'>
      items: Annex['items']
    }
    const merged: Annex = {
      label: annexFile.annex.label,
      heading: annexFile.annex.heading,
      summary: annexFile.annex.summary,
      effectiveDate: annexFile.annex.effectiveDate,
      items: annexFile.items,
    }
    // Upsert by label so re-running with a tweaked annex file doesn't append.
    const existing = snapshot.annexes ?? []
    const without = existing.filter((a) => a.label !== merged.label)
    snapshot.annexes = [...without, merged]

    console.log(
      `  Annex ${merged.label}: ${merged.items.length} items, effectiveDate = ${merged.effectiveDate}`,
    )
  } else {
    console.log(
      `wire-annex-and-dates: ${annexAbs} not found — relying on existing annexes[] in snapshot`,
    )
  }

  // Two-space indent + trailing newline matches the agent's output convention.
  writeFileSync(aiActAbs, JSON.stringify(snapshot, null, 2) + '\n', 'utf8')

  // Tear down the standalone annex file — its content now lives in the
  // main snapshot. Keeps `data/corpus/` from accumulating sources that
  // overlap with the canonical regulation file.
  if (existsSync(annexAbs)) {
    rmSync(annexAbs)
    console.log(
      `wire-annex-and-dates: removed ${annexAbs} (merged into ${AI_ACT_PATH})`,
    )
  }

  console.log('wire-annex-and-dates: done.')
}

try {
  main()
} catch (err) {
  console.error('wire-annex-and-dates: failed:')
  console.error(err instanceof Error ? (err.stack ?? err.message) : String(err))
  exit(1)
}
