/**
 * Provider-agnostic websearch / URL-fetch abstraction (ENT-98).
 *
 * # NOTHING CALLS THIS, WHICH IS RECORDED HERE RATHER THAN LEFT TO REDERIVE
 *
 * The abstraction had exactly one caller, `lib/corpus/resolve.ts`. It turned a
 * citation key into a EUR-Lex ELI anchor and asked a provider to fetch the
 * verbatim Official Journal text behind it. That resolver went with the
 * Supabase-era console at `2a5c454`, so there has been no caller since.
 *
 * The purpose did not merely lose its caller, it was answered a different way.
 * ENT-207 put the corpus in Postgres, and each obligation row carries a
 * curated summary rather than Official Journal wording. The obligation page
 * says so in as many words ("A summary, not the official wording") and links
 * out to EUR-Lex instead. That is the better answer rather than a gap: the
 * link is honest about provenance, it costs no egress, and ENT-218's citation
 * validator already requires a citation to resolve to a **stored** obligation
 * or be refused. `components/corpus/obligation-list.tsx` carries the full
 * reasoning.
 *
 * # WHY IT IS STILL HERE
 *
 * ENT-240 rules that the seam stays and gets finished rather than retired,
 * because air-gapped operation is now a stated product property and this is
 * where the one permitted outbound fetch would live: keeping the regulatory
 * corpus current. A named, configurable, auditable source that an operator
 * chose is a different thing from a library quietly calling a SaaS at citation
 * time, and the distinction is the whole point.
 *
 * Both providers are now real, and the default names `firecrawl`, the only one
 * that can run inside an air-gapped deployment. `index.ts` carries that
 * reasoning.
 *
 * # WHAT IS STILL OPEN, AND IT IS NOT A CODE QUESTION
 *
 * Whether the corpus refresh actually consumes this interface has not been
 * decided, and it should not be decided by whoever writes the refresh first.
 * `fetchUrl` returns a page as markdown, which is the right shape for reading
 * a document and the wrong shape for maintaining a corpus of articles,
 * paragraphs, recitals and annexes: EUR-Lex publishes that structure as XML
 * through Cellar, and scraping the rendered page back into it would be a
 * parser with more ways to be subtly wrong than the thing it replaced. The
 * options and their consequences are in the ENT-240 pull request. Until that
 * is ruled on, this stays an interface with no consumer, and adding one is the
 * decision rather than the implementation.
 *
 * So: treat the module as unused. No runtime path reaches it and no
 * self-hoster needs a key or an instance for it, which is why every variable
 * that configures it is documented as optional and none as required.
 *
 * # THE INTERFACE
 *
 * Intentionally minimal: `fetchUrl` is enough for citation. A `search(query)`
 * method may be added later for the Watcher's regulatory-change monitoring,
 * and appending to the interface will not break existing callers.
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
 * Thrown when a provider call fails for any reason: network error, non-2xx
 * HTTP, malformed response body, and so on. The cause chain preserves the
 * underlying error so server logs can correlate.
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
