import {
  WebSearchProviderError,
  type FetchUrlOptions,
  type FetchUrlResult,
  type WebSearchProvider,
} from './types'

/**
 * Firecrawl provider (ENT-98, implemented in ENT-240).
 *
 * # WHY THIS ONE IS THE DEFAULT
 *
 * The two providers are not interchangeable on the property this deployment
 * cares about. Firecrawl's engine is AGPL-3.0 and self-hostable, so it can run
 * inside an air-gapped install; Tavily's is closed and hosted, with no
 * self-hosting path at all, so it cannot. ENT-240 makes air-gapped operation a
 * stated product property, which settles which provider the default should
 * name: this one, with Tavily demoted to the hosted convenience for operators
 * who would rather not run a crawler stack.
 *
 * # THE SELF-HOSTED INSTANCE NEEDS NO KEY, AND THAT SHAPES THE CONSTRUCTOR
 *
 * Firecrawl's own compose stack runs with `USE_DB_AUTHENTICATION=false` and
 * accepts unauthenticated requests, so an air-gapped operator has a base URL
 * and no credential. A constructor that demanded a key would make the one
 * provider that can run air-gapped impossible to configure air-gapped, which
 * is why `baseUrl` alone is enough here and `apiKey` alone is enough too. What
 * is refused is neither: falling back to the hosted API with no credential
 * would surface as a 401 one layer away from the mistake.
 *
 * # THE REQUEST
 *
 * `POST {base}/v2/scrape` with `{ url, formats: ['markdown'] }` and an
 * `Authorization: Bearer` header when there is a key. The response is
 * `{ success, data: { markdown, metadata: { title, sourceURL, statusCode } } }`.
 * `formats` takes the string shorthand, which both v1 and v2 accept, so the
 * body is the same against either; only the path differs, and the path is a
 * constant here. An instance old enough to serve `/v1/scrape` and not
 * `/v2/scrape` would need a lever this module does not have yet, and adding
 * one before anybody has that instance is guessing.
 *
 * `onlyMainContent: true` is sent explicitly rather than left to Firecrawl's
 * default, so a change to that default does not silently change what a
 * citation contains.
 *
 * The call is bounded by an `AbortController` on our side rather than by
 * Firecrawl's own `timeout` parameter. Sending both would mean two budgets
 * that can disagree, and only one of them stops us waiting.
 *
 * # WHAT HAS NOT BEEN PROVED
 *
 * This has never been run against a real Firecrawl instance, hosted or
 * self-hosted. The request and the response handling are written against the
 * published API shape and pinned by unit tests over a stubbed `fetch`, which
 * catches a regression in this file and would not catch a difference between
 * the documentation and a running build. Nothing calls this module yet, so no
 * end-to-end path would catch it either.
 */

const FIRECRAWL_HOSTED_BASE_URL = 'https://api.firecrawl.dev'
const SCRAPE_PATH = '/v2/scrape'
const DEFAULT_TIMEOUT_MS = 30_000

export type FirecrawlProviderOptions = {
  /**
   * Base URL of a Firecrawl instance, without a path: `http://firecrawl:3002`
   * for the self-hosted image. Defaults to the hosted API, which is the only
   * case that requires a key.
   */
  baseUrl?: string
  /**
   * Bearer credential. Required for the hosted API, optional for a self-hosted
   * instance, which typically runs with authentication switched off.
   */
  apiKey?: string
}

type FirecrawlScrapeResponse = {
  success?: boolean
  error?: string
  data?: {
    markdown?: string
    metadata?: {
      title?: string | string[]
      sourceURL?: string
      url?: string
      statusCode?: number
    }
  }
}

function firstTitle(title: string | string[] | undefined): string | null {
  if (Array.isArray(title)) return title[0] ?? null
  return title ?? null
}

export function createFirecrawlProvider(
  options: FirecrawlProviderOptions,
): WebSearchProvider {
  if (!options.baseUrl && !options.apiKey) {
    // Fail loud at construction rather than at the first call, for the same
    // reason the Tavily provider does: a citation surface that returns empty
    // content is worse than one that refuses.
    throw new WebSearchProviderError(
      'firecrawl',
      'needs either a self-hosted instance (set FIRECRAWL_API_URL) or a key for the hosted API (set FIRECRAWL_API_KEY)',
    )
  }

  const base = (options.baseUrl ?? FIRECRAWL_HOSTED_BASE_URL).replace(
    /\/+$/,
    '',
  )
  const endpoint = `${base}${SCRAPE_PATH}`

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (options.apiKey) {
    headers.Authorization = `Bearer ${options.apiKey}`
  }

  return {
    name: 'firecrawl',
    async fetchUrl(
      url: string,
      fetchOptions?: FetchUrlOptions,
    ): Promise<FetchUrlResult> {
      const timeoutMs = fetchOptions?.timeoutMs ?? DEFAULT_TIMEOUT_MS
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), timeoutMs)

      let response: Response
      try {
        response = await fetch(endpoint, {
          method: 'POST',
          headers,
          body: JSON.stringify({
            url,
            formats: ['markdown'],
            onlyMainContent: true,
          }),
          signal: controller.signal,
        })
      } catch (err) {
        throw new WebSearchProviderError(
          'firecrawl',
          `network error fetching ${url}: ${err instanceof Error ? err.message : String(err)}`,
          { cause: err },
        )
      } finally {
        clearTimeout(timer)
      }

      if (!response.ok) {
        const text = await response.text().catch(() => '<unreadable body>')
        throw new WebSearchProviderError(
          'firecrawl',
          `HTTP ${response.status} ${response.statusText}: ${text}`,
        )
      }

      let payload: FirecrawlScrapeResponse
      try {
        payload = (await response.json()) as FirecrawlScrapeResponse
      } catch (err) {
        throw new WebSearchProviderError(
          'firecrawl',
          'malformed JSON response',
          { cause: err },
        )
      }

      // Firecrawl answers 200 with `success: false` for a page it could not
      // scrape, so the status alone is not the answer.
      if (payload.success === false) {
        throw new WebSearchProviderError(
          'firecrawl',
          `scrape failed for ${url}: ${payload.error ?? 'no reason given'}`,
        )
      }

      const content = payload.data?.markdown
      if (!content) {
        throw new WebSearchProviderError(
          'firecrawl',
          `no content returned for ${url}`,
        )
      }

      const metadata = payload.data?.metadata
      return {
        url: metadata?.sourceURL ?? metadata?.url ?? url,
        title: firstTitle(metadata?.title),
        content,
        fetchedAt: new Date().toISOString(),
        provider: 'firecrawl',
      }
    },
  }
}
