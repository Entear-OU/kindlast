import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getWebSearchProvider } from '@/lib/websearch'
import { WebSearchProviderError } from '@/lib/websearch/types'

/**
 * Factory selection coverage (ENT-98).
 *
 * `getWebSearchProvider()` reads `WEBSEARCH_PROVIDER` from env and picks the
 * matching impl. Defaults to `tavily` when unset, which these tests pin as it
 * stands rather than as it should end up: ENT-240 moves the default to
 * `firecrawl`, the only provider that can run air-gapped, in the same change
 * that implements it. Mis-spelled env values fail loud rather than silently
 * defaulting, because a typo should not reach production and quietly change
 * where regulatory text is fetched from.
 */

const ORIGINAL_ENV = { ...process.env }

describe('getWebSearchProvider', () => {
  beforeEach(() => {
    process.env = { ...ORIGINAL_ENV }
    vi.restoreAllMocks()
  })

  afterEach(() => {
    process.env = { ...ORIGINAL_ENV }
  })

  it('defaults to tavily when WEBSEARCH_PROVIDER is unset', () => {
    delete process.env.WEBSEARCH_PROVIDER
    process.env.TAVILY_API_KEY = 'tvly-test-key'

    const provider = getWebSearchProvider()
    expect(provider.name).toBe('tavily')
  })

  it('returns tavily when WEBSEARCH_PROVIDER=tavily', () => {
    process.env.WEBSEARCH_PROVIDER = 'tavily'
    process.env.TAVILY_API_KEY = 'tvly-test-key'

    const provider = getWebSearchProvider()
    expect(provider.name).toBe('tavily')
  })

  it('returns firecrawl stub when WEBSEARCH_PROVIDER=firecrawl', () => {
    process.env.WEBSEARCH_PROVIDER = 'firecrawl'
    process.env.FIRECRAWL_API_KEY = 'fc-test-key'

    const provider = getWebSearchProvider()
    expect(provider.name).toBe('firecrawl')
  })

  it('throws when WEBSEARCH_PROVIDER is set to an unknown value (no silent fallback)', () => {
    process.env.WEBSEARCH_PROVIDER = 'duckduckgo'
    process.env.TAVILY_API_KEY = 'tvly-test-key'

    expect(() => getWebSearchProvider()).toThrow(/WEBSEARCH_PROVIDER/i)
  })

  it('throws WebSearchProviderError when the selected provider has no API key', () => {
    process.env.WEBSEARCH_PROVIDER = 'tavily'
    delete process.env.TAVILY_API_KEY

    expect(() => getWebSearchProvider()).toThrow(WebSearchProviderError)
  })

  it('accepts an explicit override (useful for tests / DI)', () => {
    process.env.WEBSEARCH_PROVIDER = 'tavily'
    process.env.TAVILY_API_KEY = 'env-key'

    // Passing apiKey overrides the env var.
    const provider = getWebSearchProvider({
      provider: 'tavily',
      apiKey: 'explicit-key',
    })
    expect(provider.name).toBe('tavily')
  })
})
