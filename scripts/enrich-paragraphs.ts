#!/usr/bin/env tsx
/**
 * Offline enrichment script (ENT-95): walk the EU AI Act snapshot and
 * append a `paragraphs[]` array to each MVP-critical article using the
 * pure parser in `lib/corpus/paragraphs.ts`. Writes back to the source
 * file in place. Idempotent — re-running produces byte-identical output
 * as long as the source `body` and the parser haven't changed.
 *
 * The enricher only touches the MVP-critical articles (4, 6, 9–17, 26,
 * 50). Other articles keep `body` alone — adding paragraphs there is
 * non-zero work to verify per article, and ENT-95's scope is just the
 * Analyst's MVP citation surface.
 *
 * Usage:
 *
 *   pnpm enrich:ai-act-paragraphs
 *   # or directly:
 *   tsx scripts/enrich-paragraphs.ts data/corpus/eu-ai-act.json
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { argv, cwd, exit } from 'node:process'

import { splitParagraphs } from '../lib/corpus/paragraphs'

const MVP_CRITICAL_ARTICLES = new Set([4, 6, 9, 10, 11, 12, 13, 14, 15, 16, 17, 26, 50])

const DEFAULT_PATH = 'data/corpus/eu-ai-act.json'

type Article = {
  articleNumber: number
  heading: string
  body: string
  paragraphs?: Array<{ label: string; body: string; ordering: number }>
}

type Snapshot = {
  document: unknown
  articles: Article[]
  recitals: unknown[]
}

function main(): void {
  const path = argv[2] ?? DEFAULT_PATH
  const absolute = resolve(cwd(), path)
  console.log(`enrich-paragraphs: reading ${absolute}`)

  const raw = readFileSync(absolute, 'utf8')
  const data = JSON.parse(raw) as Snapshot

  let enrichedCount = 0
  let paragraphRowCount = 0
  for (const article of data.articles) {
    if (!MVP_CRITICAL_ARTICLES.has(article.articleNumber)) {
      // Make sure we don't accidentally leave stale paragraphs on non-MVP
      // articles from an earlier enrichment with a wider scope.
      delete article.paragraphs
      continue
    }
    const paragraphs = splitParagraphs(article.body)
    article.paragraphs = paragraphs
    enrichedCount += 1
    paragraphRowCount += paragraphs.length
    console.log(
      `  article ${article.articleNumber} (${article.heading}): ${paragraphs.length} paragraph rows`,
    )
  }

  // Pretty-print so future diffs are readable. Two-space indent matches the
  // existing file shape (the agent that fetched the snapshot wrote it that
  // way — keep parity to avoid churn-only diffs).
  const out = JSON.stringify(data, null, 2) + '\n'
  writeFileSync(absolute, out, 'utf8')

  console.log(
    `enrich-paragraphs: enriched ${enrichedCount} articles with ${paragraphRowCount} paragraph rows total.`,
  )
}

try {
  main()
} catch (err) {
  console.error('enrich-paragraphs: failed:')
  console.error(err instanceof Error ? err.stack ?? err.message : String(err))
  exit(1)
}
