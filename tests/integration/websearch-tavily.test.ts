// @vitest-environment node
import { describe, expect, it } from 'vitest'

import { createTavilyProvider } from '@/lib/websearch/tavily'
import { fetchCitation } from '@/lib/corpus/resolve'

/**
 * Live Tavily integration coverage (ENT-98).
 *
 * Makes one real Extract call against EUR-Lex via Tavily — the rest of
 * the suite uses mocked fetch (`__tests__/lib/websearch/tavily.test.ts`)
 * so CI doesn't depend on credentials. Skipped when `TAVILY_API_KEY` is
 * not set in env, exactly mirroring the supabase-running skipif pattern
 * used elsewhere in this folder.
 *
 * Why this exists at all: mocked tests prove our request shape and our
 * response parsing. Only a live call proves Tavily's API still answers
 * the shape we expect — and that the EUR-Lex ELI anchor URL we
 * construct in `lib/corpus/resolve.ts` actually returns the right
 * article. The cost of one call per CI run is worth that confidence.
 */

const TAVILY_API_KEY = process.env.TAVILY_API_KEY ?? ''
const liveSkip = !TAVILY_API_KEY

describe.skipIf(liveSkip)('Tavily live integration (ENT-98)', () => {
  it('extracts content from a regulation page via fetchCitation', async () => {
    const provider = createTavilyProvider({ apiKey: TAVILY_API_KEY })
    const result = await fetchCitation(
      { kind: 'article', celex: '32016R0679', articleNumber: 6 },
      provider,
      { timeoutMs: 30_000 },
    )

    expect(result.provider).toBe('tavily')
    expect(result.url).toContain('eur-lex.europa.eu')
    expect(result.content.length).toBeGreaterThan(200)
    // Article 6 of GDPR is "Lawfulness of processing" — the extracted text
    // for the article page should mention one of the lawful bases somewhere.
    expect(result.content.toLowerCase()).toMatch(/lawful|consent|legitimate/)
  })
})
