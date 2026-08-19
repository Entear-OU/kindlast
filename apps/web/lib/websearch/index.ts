/**
 * Websearch provider factory (ENT-98).
 *
 * Reads `WEBSEARCH_PROVIDER` (default `firecrawl`) from env and returns the
 * matching implementation. The caller can override via the optional argument,
 * which is useful for tests, DI, and the rare case where an admin surface
 * needs to A/B providers in-process.
 *
 * The factory is the only place that touches env vars; callers pass the
 * resulting `WebSearchProvider` around as a value, which keeps `fetchUrl`
 * call sites pure and trivially mockable.
 *
 * # WHY THE DEFAULT IS FIRECRAWL (ENT-240)
 *
 * It used to be `tavily`, and moving it is the point of ENT-240 rather than a
 * preference between two vendors. Air-gapped operation is a stated product
 * property, and only one of these two providers can run inside such a
 * deployment: Firecrawl's engine is AGPL-3.0 and self-hostable, Tavily's is
 * closed and hosted with no self-hosting path. A default that named the
 * provider structurally incapable of the property the product claims is the
 * kind of small wrong default that becomes an assumption.
 *
 * The default moved in the same change that implemented the Firecrawl fetch,
 * deliberately and not before: pointing the default at a provider that threw
 * `not implemented` would have traded an honest state for a broken one.
 *
 * # NOTHING CALLS THIS FACTORY TODAY
 *
 * `types.ts` records why, and what the seam is being kept for. Because there
 * is no caller, no key and no instance URL is required to run this deployment,
 * and the self-hosting guide says so. Configuring one is how an operator opts
 * the seam in, which is the shape the air-gap position asks for: a named
 * source somebody chose, not ambient egress.
 */

import { createFirecrawlProvider } from './firecrawl'
import { createTavilyProvider } from './tavily'
import {
  WebSearchProviderError,
  type WebSearchProvider,
  type WebSearchProviderName,
} from './types'

export type GetProviderOptions = {
  provider?: WebSearchProviderName
  apiKey?: string
  /** Firecrawl only: base URL of a self-hosted instance. */
  baseUrl?: string
}

const KNOWN_PROVIDERS: ReadonlyArray<WebSearchProviderName> = [
  'tavily',
  'firecrawl',
]

const DEFAULT_PROVIDER: WebSearchProviderName = 'firecrawl'

function resolveProviderName(
  explicit?: WebSearchProviderName,
): WebSearchProviderName {
  const value = explicit ?? process.env.WEBSEARCH_PROVIDER ?? DEFAULT_PROVIDER
  if (!KNOWN_PROVIDERS.includes(value as WebSearchProviderName)) {
    throw new Error(
      `WEBSEARCH_PROVIDER=${value} is not a known provider (one of: ${KNOWN_PROVIDERS.join(', ')})`,
    )
  }
  return value as WebSearchProviderName
}

export function getWebSearchProvider(
  options?: GetProviderOptions,
): WebSearchProvider {
  const name = resolveProviderName(options?.provider)

  switch (name) {
    case 'tavily': {
      const apiKey = options?.apiKey ?? process.env.TAVILY_API_KEY ?? ''
      if (!apiKey) {
        throw new WebSearchProviderError(
          'tavily',
          'API key is required (set TAVILY_API_KEY)',
        )
      }
      return createTavilyProvider({ apiKey })
    }
    case 'firecrawl': {
      // Either is enough, and the provider refuses when neither is set. A
      // self-hosted instance usually wants only the URL, because the image
      // runs with authentication switched off; the hosted API wants only the
      // key. Both together is a self-hosted instance that was configured with
      // a credential.
      return createFirecrawlProvider({
        baseUrl: options?.baseUrl ?? process.env.FIRECRAWL_API_URL ?? undefined,
        apiKey: options?.apiKey ?? process.env.FIRECRAWL_API_KEY ?? undefined,
      })
    }
  }
}

export { WebSearchProviderError } from './types'
export type {
  FetchUrlOptions,
  FetchUrlResult,
  WebSearchProvider,
  WebSearchProviderName,
} from './types'
