import { describe, expect, it, vi } from 'vitest'

import { citationKeyToUrl, fetchCitation } from '@/lib/corpus/resolve'
import type { FetchUrlResult, WebSearchProvider } from '@/lib/websearch/types'

/**
 * Unit coverage for `citationKeyToUrl` and `fetchCitation` (ENT-98).
 *
 * The resolver is the bridge between the corpus catalog and the runtime
 * fetch path. A `CitationKey` (natural-key + kind) becomes an EUR-Lex
 * ELI anchor URL; the URL is handed to a provider. The mapping is pure
 * — no DB calls — so the suite is a fast unit suite.
 *
 * CELEX → ELI mapping:
 *   * `32016R0679` (GDPR)        → eli/reg/2016/679/oj
 *   * `32024R1689` (EU AI Act)   → eli/reg/2024/1689/oj
 * General regex: `^3(\d{4})R(\d{4})$` → eli/reg/{year}/{number}/oj
 *
 * Anchor fragments (per EUR-Lex's published ELI format):
 *   * Article N         → #art_N
 *   * Recital N         → #rct_N
 *   * Annex (label)     → #anx_{label}
 *   * Annex item        → #anx_{annex}_{label} (best-effort; EUR-Lex's
 *     deep-link structure for annex items is less stable than for
 *     articles/recitals — the provider fetches the annex page either way
 *     and the LLM picks the item from extracted content).
 */

describe('citationKeyToUrl', () => {
  it('maps an article citation (GDPR) to eli/reg + #art_N', () => {
    const url = citationKeyToUrl({ kind: 'article', celex: '32016R0679', articleNumber: 6 })
    expect(url).toBe('https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_6')
  })

  it('maps an article citation (AI Act) to eli/reg + #art_N', () => {
    const url = citationKeyToUrl({ kind: 'article', celex: '32024R1689', articleNumber: 6 })
    expect(url).toBe('https://eur-lex.europa.eu/eli/reg/2024/1689/oj#art_6')
  })

  it('article + paragraph label produces the same article anchor (EUR-Lex does not deep-link to sub-paragraphs)', () => {
    // Sub-paragraph addressing happens in the catalog row (paragraph_label)
    // — the fetched URL is the article. The LLM uses the paragraph_label
    // when scanning the fetched text.
    const url = citationKeyToUrl({
      kind: 'article',
      celex: '32024R1689',
      articleNumber: 6,
      paragraphLabel: '1(a)',
    })
    expect(url).toBe('https://eur-lex.europa.eu/eli/reg/2024/1689/oj#art_6')
  })

  it('maps a recital citation to eli/reg + #rct_N', () => {
    const url = citationKeyToUrl({ kind: 'recital', celex: '32016R0679', recitalNumber: 47 })
    expect(url).toBe('https://eur-lex.europa.eu/eli/reg/2016/679/oj#rct_47')
  })

  it('maps an annex citation to eli/reg + #anx_label', () => {
    const url = citationKeyToUrl({ kind: 'annex', celex: '32024R1689', annexLabel: 'III' })
    expect(url).toBe('https://eur-lex.europa.eu/eli/reg/2024/1689/oj#anx_III')
  })

  it('maps an annex item citation to the annex page (item label is for LLM disambiguation)', () => {
    const url = citationKeyToUrl({
      kind: 'annex',
      celex: '32024R1689',
      annexLabel: 'III',
      itemLabel: '1(a)',
    })
    expect(url).toBe('https://eur-lex.europa.eu/eli/reg/2024/1689/oj#anx_III')
  })

  it('rejects a malformed CELEX (we want a clear error, not a 404 from EUR-Lex)', () => {
    expect(() =>
      citationKeyToUrl({
        kind: 'article',
        // Missing the leading sector digit; not a real CELEX number.
        celex: '2016R0679',
        articleNumber: 6,
      }),
    ).toThrow(/celex/i)
  })

  it('rejects a non-positive article number', () => {
    expect(() =>
      citationKeyToUrl({ kind: 'article', celex: '32016R0679', articleNumber: 0 }),
    ).toThrow()
  })
})

describe('fetchCitation', () => {
  function makeFakeProvider(content: string): WebSearchProvider {
    const fetchUrl = vi.fn(async (url: string): Promise<FetchUrlResult> => ({
      url,
      title: null,
      content,
      fetchedAt: '2026-05-27T00:00:00.000Z',
      provider: 'tavily',
    }))
    return { name: 'tavily', fetchUrl }
  }

  it('resolves the key to a URL and calls the provider', async () => {
    const provider = makeFakeProvider('Article 6 — Lawfulness of processing\n…')
    const result = await fetchCitation(
      { kind: 'article', celex: '32016R0679', articleNumber: 6 },
      provider,
    )
    expect(result.content).toContain('Lawfulness of processing')
    expect(provider.fetchUrl).toHaveBeenCalledWith(
      'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_6',
      undefined,
    )
  })

  it('passes timeout options through to the provider', async () => {
    const provider = makeFakeProvider('x'.repeat(200))
    await fetchCitation(
      { kind: 'recital', celex: '32024R1689', recitalNumber: 47 },
      provider,
      { timeoutMs: 5000 },
    )
    expect(provider.fetchUrl).toHaveBeenCalledWith(expect.any(String), { timeoutMs: 5000 })
  })
})
