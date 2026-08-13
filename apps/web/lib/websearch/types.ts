/**
 * Provider-agnostic websearch / URL-fetch abstraction (ENT-98).
 *
 * The Analyst pulls verbatim regulatory text from a known `source_url` at
 * citation time — closing the loop on the progressive disclosure
 * architecture (ENT-32). Tavily and Firecrawl are the candidate providers;
 * locking call sites to one vendor is the lock-in this interface exists
 * to prevent. Concrete implementations live in `tavily.ts` and
 * `firecrawl.ts` and are selected by `index.ts` from the
 * `WEBSEARCH_PROVIDER` env var.
 *
 * Interface is intentionally minimal: `fetchUrl` is enough for citation.
 * A `search(query)` method may be added later for the Watcher's
 * regulatory-change monitoring; appending to the interface won't break
 * existing callers.
 */

export type WebSearchProviderName = 'tavily' | 'firecrawl'

export type FetchUrlOptions = {
  /**
   * Maximum wall-clock time to wait for the provider. Defaults to 30 seconds.
   * The Analyst typically calls this from a server handler with its own
   * timeout budget, so we keep this conservative.
   */
  timeoutMs?: number
}

export type FetchUrlResult = {
  /** Final URL after redirects (provider's view). */
  url: string
  /** Page title if the provider extracted one; null when the source has none. */
  title: string | null
  /**
   * Extracted body text. Format is provider-dependent but always plain
   * UTF-8 string ready to feed into an LLM prompt. Implementations
   * normalise CR/LF and strip the most aggressive whitespace, but do not
   * promise byte-identical output between providers.
   */
  content: string
  /** ISO-8601 timestamp at which the fetch was performed. */
  fetchedAt: string
  /** Which concrete provider served this result. Useful for audit logs. */
  provider: WebSearchProviderName
}

export interface WebSearchProvider {
  readonly name: WebSearchProviderName
  fetchUrl(url: string, options?: FetchUrlOptions): Promise<FetchUrlResult>
}

/**
 * Thrown when a provider call fails for any reason — network error,
 * non-2xx HTTP, malformed response body, etc. The cause chain preserves
 * the underlying error so server logs can correlate.
 */
export class WebSearchProviderError extends Error {
  constructor(
    public readonly provider: WebSearchProviderName,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(`${provider}: ${message}`, options)
    this.name = 'WebSearchProviderError'
  }
}
