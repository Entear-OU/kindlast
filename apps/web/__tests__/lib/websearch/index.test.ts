import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getWebSearchProvider } from '@/lib/websearch'
import { WebSearchProviderError } from '@/lib/websearch/types'

/**
 * Factory selection coverage (ENT-98, default moved in ENT-240).
 *
 * `getWebSearchProvider()` reads `WEBSEARCH_PROVIDER` from env and picks the
 * matching impl. It defaults to `firecrawl`, the only provider that can run
 * inside an air-gapped deployment, and these tests pin that: a default that
 * quietly went back to the hosted-only provider would take the product
 * property with it. Mis-spelled env values fail loud rather than silently
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

  it('defaults to firecrawl when WEBSEARCH_PROVIDER is unset', () => {
    delete process.env.WEBSEARCH_PROVIDER
    process.env.FIRECRAWL_API_URL = 'http://firecrawl:3002'

    const provider = getWebSearchProvider()
    expect(provider.name).toBe('firecrawl')
  })

  it('returns tavily when WEBSEARCH_PROVIDER=tavily', () => {
    process.env.WEBSEARCH_PROVIDER = 'tavily'
    process.env.TAVILY_API_KEY = 'tvly-test-key'

    const provider = getWebSearchProvider()
    expect(provider.name).toBe('tavily')
  })

  it('returns firecrawl when WEBSEARCH_PROVIDER=firecrawl', () => {
    process.env.WEBSEARCH_PROVIDER = 'firecrawl'
    process.env.FIRECRAWL_API_URL = 'http://firecrawl:3002'

    const provider = getWebSearchProvider()
    expect(provider.name).toBe('firecrawl')
  })

  it('builds firecrawl from a key alone, for the hosted API', () => {
    process.env.WEBSEARCH_PROVIDER = 'firecrawl'
    delete process.env.FIRECRAWL_API_URL
    process.env.FIRECRAWL_API_KEY = 'fc-test-key'

    expect(getWebSearchProvider().name).toBe('firecrawl')
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

  it('throws when firecrawl has neither an instance URL nor a key', () => {
    // The default provider with no configuration at all refuses rather than
    // silently reaching for the hosted API, which is the behaviour the
    // air-gap position asks for: a source an operator named, or nothing.
    process.env.WEBSEARCH_PROVIDER = 'firecrawl'
    delete process.env.FIRECRAWL_API_URL
    delete process.env.FIRECRAWL_API_KEY

    expect(() => getWebSearchProvider()).toThrow(WebSearchProviderError)
    expect(() => getWebSearchProvider()).toThrow(/FIRECRAWL_API_URL/)
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

  it('accepts an explicit firecrawl base URL, overriding the env var', () => {
    process.env.WEBSEARCH_PROVIDER = 'firecrawl'
    process.env.FIRECRAWL_API_URL = 'http://from-env:3002'

    const provider = getWebSearchProvider({
      provider: 'firecrawl',
      baseUrl: 'http://explicit:3002',
    })
    expect(provider.name).toBe('firecrawl')
  })
})
