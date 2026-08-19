import {
  WebSearchProviderError,
  type FetchUrlResult,
  type WebSearchProvider,
} from './types'

/**
 * Firecrawl provider, still a stub (ENT-98, to be finished in ENT-240).
 *
 * Kept as a compile-time placeholder so the factory in `index.ts` can branch
 * on `WEBSEARCH_PROVIDER=firecrawl` without a runtime crash, and so a future
 * implementation has an obvious file to flesh out. Calling `fetchUrl` throws,
 * because failing loud beats degrading silently if someone flips the env var
 * before the implementation lands. It ignores its arguments deliberately and
 * so declares none.
 *
 * # WHY THIS ONE IS THE INTENDED DEFAULT, EVEN THOUGH TAVILY STILL IS
 *
 * The two providers are not interchangeable on the property this deployment
 * cares about. Firecrawl's engine is AGPL-3.0 and self-hostable, so it can run
 * inside an air-gapped install; Tavily's is closed and hosted, with no
 * self-hosting path at all, so it cannot. ENT-240 makes air-gapped operation a
 * stated product property, which settles which provider should be the default:
 * this one, with Tavily demoted to the hosted convenience for operators who do
 * not want to run a crawler stack.
 *
 * The default in `index.ts` has **not** moved yet, and that is on purpose.
 * Pointing the default at a provider that throws would trade an honest state
 * for a broken one, and there is no caller to benefit either way. The default
 * moves in the same change that implements the fetch, not before it.
 *
 * When implementing for real, target the Firecrawl `/v1/scrape` endpoint with
 * `formats: ['markdown']`, make the base URL configurable so a self-hosted
 * instance can be pointed at, and shape the response into a `FetchUrlResult`.
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
    async fetchUrl(): Promise<FetchUrlResult> {
      throw new WebSearchProviderError(
        'firecrawl',
        'not implemented yet (ENT-240). Set WEBSEARCH_PROVIDER=tavily until the Firecrawl implementation lands',
      )
    },
  }
}
