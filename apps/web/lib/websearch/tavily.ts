import {
  WebSearchProviderError,
  type FetchUrlOptions,
  type FetchUrlResult,
  type WebSearchProvider,
} from './types'

/**
 * Tavily provider (ENT-98).
 *
 * Uses the Extract API: POST /extract with `{ urls: [<single url>] }` and
 * `Authorization: Bearer <api-key>`. The response shape is
 *
 *   { results: [{ url, raw_content, ... }], failed_results: [...], response_time }
 *
 * We pass a single URL at a time and surface failure modes as
 * `WebSearchProviderError` so callers see a uniform error type regardless
 * of provider. Network errors, non-2xx statuses, malformed bodies, and
 * URLs that Tavily can't extract all funnel into the same error.
 */

const TAVILY_EXTRACT_URL = 'https://api.tavily.com/extract'
const DEFAULT_TIMEOUT_MS = 30_000

export type TavilyProviderOptions = {
  apiKey: string
  /**
   * Override the API endpoint. Production code never sets this; integration
   * tests + future regional endpoints might.
   */
  endpoint?: string
}

type TavilyExtractResponse = {
  results: Array<{ url: string; raw_content: string }>
  failed_results: Array<{ url: string; error: string }>
  response_time?: number
}

export function createTavilyProvider(
  options: TavilyProviderOptions,
): WebSearchProvider {
  if (!options.apiKey) {
    // Fail loud at construction rather than at the first call. A missing
    // key with a silent no-op would look "successful" but return zero
    // content, which is exactly the worst failure mode for a citation surface.
    throw new WebSearchProviderError(
      'tavily',
      'API key is required (set TAVILY_API_KEY)',
    )
  }

  const endpoint = options.endpoint ?? TAVILY_EXTRACT_URL

  return {
    name: 'tavily',
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
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${options.apiKey}`,
          },
          body: JSON.stringify({ urls: [url] }),
          signal: controller.signal,
        })
      } catch (err) {
        clearTimeout(timer)
        throw new WebSearchProviderError(
          'tavily',
          `network error fetching ${url}: ${err instanceof Error ? err.message : String(err)}`,
          { cause: err },
        )
      }
      clearTimeout(timer)

      if (!response.ok) {
        const text = await response.text().catch(() => '<unreadable body>')
        throw new WebSearchProviderError(
          'tavily',
          `HTTP ${response.status} ${response.statusText}: ${text}`,
        )
      }

      let payload: TavilyExtractResponse
      try {
        payload = (await response.json()) as TavilyExtractResponse
      } catch (err) {
        throw new WebSearchProviderError('tavily', 'malformed JSON response', {
          cause: err,
        })
      }

      const failed = payload.failed_results?.find((f) => f.url === url)
      if (failed) {
        throw new WebSearchProviderError(
          'tavily',
          `extract failed for ${url}: ${failed.error}`,
        )
      }

      const hit =
        payload.results?.find((r) => r.url === url) ?? payload.results?.[0]
      if (!hit || !hit.raw_content) {
        throw new WebSearchProviderError(
          'tavily',
          `no result returned for ${url}`,
        )
      }

      return {
        url: hit.url,
        title: null,
        content: hit.raw_content,
        fetchedAt: new Date().toISOString(),
        provider: 'tavily',
      }
    },
  }
}
