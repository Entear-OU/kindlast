import {
  WebSearchProviderError,
  type FetchUrlOptions,
  type FetchUrlResult,
  type WebSearchProvider,
} from './types'

/**
 * Firecrawl provider — stub (ENT-98).
 *
 * Kept as a compile-time placeholder so the factory in `index.ts` can
 * branch on `WEBSEARCH_PROVIDER=firecrawl` without a runtime crash, and
 * so a future implementation has an obvious file to flesh out. Calling
 * `fetchUrl` throws — we'd rather fail loud than silently degrade if
 * someone flips the env var before the impl lands.
 *
 * When implementing for real, target the Firecrawl `/v1/scrape` endpoint
 * with `formats: ['markdown']` and shape the response into a
 * `FetchUrlResult`.
 */

export type FirecrawlProviderOptions = {
  apiKey: string
}

export function createFirecrawlProvider(
  options: FirecrawlProviderOptions,
): WebSearchProvider {
  if (!options.apiKey) {
    throw new WebSearchProviderError(
      'firecrawl',
      'API key is required (set FIRECRAWL_API_KEY)',
    )
  }
  return {
    name: 'firecrawl',
    async fetchUrl(_url: string, _opts?: FetchUrlOptions): Promise<FetchUrlResult> {
      throw new WebSearchProviderError(
        'firecrawl',
        'not implemented yet — set WEBSEARCH_PROVIDER=tavily until the Firecrawl impl lands',
      )
    },
  }
}
