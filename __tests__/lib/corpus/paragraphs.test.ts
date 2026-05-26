import { describe, expect, it } from 'vitest'

import { splitParagraphs } from '@/lib/corpus/paragraphs'

/**
 * Pure-function coverage for `splitParagraphs` — the offline enricher that
 * walks an article body and emits structured paragraph rows for ENT-95.
 *
 * Real article bodies are committed in `data/corpus/eu-ai-act.json`. Here
 * we exercise the parser against the four legislative shapes it must
 * handle:
 *
 *   1. Plain prose (no numbering)  — Article 4 (AI literacy)
 *   2. Numbered paragraphs only     — Article 4(1), 4(2), …
 *   3. Numbered with letter sub-points — Article 6(1)(a), 6(1)(b), …
 *   4. Continuation paragraphs after a letter list — Article 6(3) "Notwithstanding the first subparagraph…"
 */

describe('splitParagraphs', () => {
  it('returns one row labelled "1" for plain prose (no numbering)', () => {
    const body = 'Providers and deployers of AI systems shall take measures to ensure a sufficient level of AI literacy.'
    expect(splitParagraphs(body)).toEqual([{ label: '1', body, ordering: 1 }])
  })

  it('splits numbered paragraphs into sequential rows', () => {
    const body = `1. Deployers shall take appropriate technical and organisational measures.

2. Deployers shall assign human oversight to natural persons.

3. The obligations are without prejudice to other deployer obligations.`
    const parsed = splitParagraphs(body)
    expect(parsed).toEqual([
      { label: '1', body: 'Deployers shall take appropriate technical and organisational measures.', ordering: 1 },
      { label: '2', body: 'Deployers shall assign human oversight to natural persons.', ordering: 2 },
      { label: '3', body: 'The obligations are without prejudice to other deployer obligations.', ordering: 3 },
    ])
  })

  it('prefixes letter sub-points with their parent number', () => {
    const body = `1. The system shall be considered high-risk where both conditions are fulfilled:

(a) the AI system is intended to be used as a safety component;

(b) the product is required to undergo a third-party conformity assessment.`
    const parsed = splitParagraphs(body)
    expect(parsed).toEqual([
      { label: '1', body: 'The system shall be considered high-risk where both conditions are fulfilled:', ordering: 1 },
      { label: '1(a)', body: 'the AI system is intended to be used as a safety component;', ordering: 2 },
      { label: '1(b)', body: 'the product is required to undergo a third-party conformity assessment.', ordering: 3 },
    ])
  })

  it('keeps independent sub-point sequences for each parent number', () => {
    // Article 9 shape: paragraph 2 has (a)–(d), paragraph 5 has (a)–(c).
    // Both label "(a)" must be addressable as "2(a)" and "5(a)".
    const body = `2. The system shall comprise:

(a) the identification of risks;

(b) the estimation and evaluation;

5. The risks shall be such that the residual risk is acceptable:

(a) elimination through design;

(b) implementation of mitigation measures.`
    const parsed = splitParagraphs(body)
    const labels = parsed.map((p) => p.label)
    expect(labels).toEqual(['2', '2(a)', '2(b)', '5', '5(a)', '5(b)'])
  })

  it('appends a continuation block to the most recent top-level row', () => {
    // Article 6(3) shape: numbered paragraph, lettered sub-points, then a
    // trailing "Notwithstanding…" that legislatively belongs to paragraph 3
    // (a "second subparagraph"), not to letter (d). Append to "3".
    const body = `3. An AI system shall not be considered high-risk where it does not pose a significant risk:

(a) the AI system performs a narrow procedural task;

(b) the AI system improves human activity.

Notwithstanding the first subparagraph, an AI system referred to in Annex III shall always be considered high-risk where it performs profiling.`
    const parsed = splitParagraphs(body)
    expect(parsed.map((p) => p.label)).toEqual(['3', '3(a)', '3(b)'])
    expect(parsed[0]!.body).toContain('shall not be considered high-risk')
    expect(parsed[0]!.body).toContain('Notwithstanding the first subparagraph')
  })

  it('returns assigns sequential ordering across all rows', () => {
    const body = `1. First.

(a) sub a.

2. Second.`
    const parsed = splitParagraphs(body)
    expect(parsed.map((p) => p.ordering)).toEqual([1, 2, 3])
  })

  it('strips trailing whitespace inside each row body', () => {
    const body = `1. Trailing whitespace.

(a) Sub with trailing whitespace.   `
    const parsed = splitParagraphs(body)
    expect(parsed[0]!.body).toBe('Trailing whitespace.')
    expect(parsed[1]!.body).toBe('Sub with trailing whitespace.')
  })

  it('returns an empty array for an empty body', () => {
    expect(splitParagraphs('')).toEqual([])
    expect(splitParagraphs('   \n\n  ')).toEqual([])
  })

  it('tolerates multiple blank lines between blocks', () => {
    const body = `1. First.



2. Second.`
    expect(splitParagraphs(body).map((p) => p.label)).toEqual(['1', '2'])
  })

  it('handles a lead-in followed by letter sub-points with no top-level number', () => {
    // Article 16 of the EU AI Act: the lead-in "Providers … shall:" sets
    // up the letter list (a)–(l), no "1." prefix. The natural OJ citation
    // is "Article 16, point (a)" — labels are bare "(a)", "(b)". The
    // lead-in itself is not citable as its own paragraph; it stays in
    // the article body (which the row's parent already preserves), so
    // the parser drops it from the paragraph list.
    const body = `Providers of high-risk AI systems shall:

(a) ensure that their high-risk AI systems are compliant;

(b) indicate on the high-risk AI system their name;

(c) have a quality management system in place.`
    const parsed = splitParagraphs(body)
    expect(parsed.map((p) => p.label)).toEqual(['(a)', '(b)', '(c)'])
    expect(parsed[0]!.body).toBe('ensure that their high-risk AI systems are compliant;')
  })
})
