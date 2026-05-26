import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { parseRegulationData } from '@/lib/corpus/ingest'

/**
 * Snapshot-shape coverage for the regulation JSON files in `data/corpus/`.
 *
 * The validator (`parseRegulationData`) and the ingest path are exercised in
 * their own suites — this is the "front gate" for the committed source data:
 * if a curator edits a corpus JSON file and breaks its shape, the build
 * surfaces it here, not at `pnpm ingest:*` time.
 *
 * Each snapshot also gets a few cheap sanity assertions on the counts and
 * known-stable content. The numbers are the official article/recital counts
 * for each regulation — they are the spec, not an arbitrary fixture.
 */

function loadSnapshot(relative: string): unknown {
  const absolute = resolve(process.cwd(), relative)
  const text = readFileSync(absolute, 'utf8')
  return JSON.parse(text)
}

describe('data/corpus/gdpr.json (ENT-48)', () => {
  it('parses with parseRegulationData and matches GDPR counts', () => {
    const data = parseRegulationData(loadSnapshot('data/corpus/gdpr.json'))
    expect(data.document.celexNumber).toBe('32016R0679')
    expect(data.document.versionDate).toBe('2016-05-04')
    expect(data.articles).toHaveLength(99)
    expect(data.recitals).toHaveLength(173)
  })
})

describe('data/corpus/eu-ai-act.json (ENT-94)', () => {
  it('parses with parseRegulationData and matches EU AI Act counts', () => {
    const data = parseRegulationData(loadSnapshot('data/corpus/eu-ai-act.json'))
    expect(data.document.celexNumber).toBe('32024R1689')
    expect(data.document.versionDate).toBe('2024-07-12')
    // EU AI Act has 113 articles in the final OJ text.
    expect(data.articles).toHaveLength(113)
    // Recital count is 180 in the consolidated text.
    expect(data.recitals).toHaveLength(180)
  })

  it('captures Article 4 (AI literacy) — the first AI Act obligation already in force', () => {
    const data = parseRegulationData(loadSnapshot('data/corpus/eu-ai-act.json'))
    const article4 = data.articles.find((a) => a.articleNumber === 4)
    expect(article4).toBeDefined()
    // Article 4's heading varies across mirrors ("AI literacy") but the
    // word literacy is invariant.
    expect(article4!.heading.toLowerCase()).toContain('literacy')
    expect(article4!.body.toLowerCase()).toContain('ai literacy')
  })

  it('enriches MVP-critical articles with addressable paragraph rows (ENT-95)', () => {
    const data = parseRegulationData(loadSnapshot('data/corpus/eu-ai-act.json'))
    const mvpCritical = [4, 6, 9, 10, 11, 12, 13, 14, 15, 16, 17, 26, 50]
    for (const articleNumber of mvpCritical) {
      const article = data.articles.find((a) => a.articleNumber === articleNumber)
      expect(article, `article ${articleNumber} missing`).toBeDefined()
      expect(
        article!.paragraphs?.length,
        `article ${articleNumber} has no paragraph rows`,
      ).toBeGreaterThanOrEqual(1)
    }
  })

  it('exposes Article 6(1)(a) and 6(1)(b) as individually addressable rows', () => {
    const data = parseRegulationData(loadSnapshot('data/corpus/eu-ai-act.json'))
    const article6 = data.articles.find((a) => a.articleNumber === 6)
    const labels = article6!.paragraphs!.map((p) => p.label)
    expect(labels).toContain('1(a)')
    expect(labels).toContain('1(b)')
  })

  it('exposes Article 16 lettered obligations (a)–(l) as individually addressable rows', () => {
    // Article 16 has no top-level paragraph number — its sub-points (a)..(l)
    // are cited directly. The parser emits bare "(letter)" labels.
    const data = parseRegulationData(loadSnapshot('data/corpus/eu-ai-act.json'))
    const article16 = data.articles.find((a) => a.articleNumber === 16)
    const labels = article16!.paragraphs!.map((p) => p.label)
    for (const letter of ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l']) {
      expect(labels, `article 16 missing (${letter})`).toContain(`(${letter})`)
    }
  })

  it('does NOT enrich non-MVP-critical articles (scope guard for ENT-95)', () => {
    // Articles outside the MVP-critical set should have no paragraphs[] field.
    // If a later sub-issue widens scope, expand the set in the enrichment
    // script (`scripts/enrich-paragraphs.ts`) — not silently here.
    const data = parseRegulationData(loadSnapshot('data/corpus/eu-ai-act.json'))
    const mvpCritical = new Set([4, 6, 9, 10, 11, 12, 13, 14, 15, 16, 17, 26, 50])
    for (const article of data.articles) {
      if (mvpCritical.has(article.articleNumber)) continue
      expect(
        article.paragraphs,
        `article ${article.articleNumber} unexpectedly has paragraphs`,
      ).toBeUndefined()
    }
  })
})
