/**
 * Websearch provider factory (ENT-98).
 *
 * Reads `WEBSEARCH_PROVIDER` (default `tavily`) from env and returns the
 * matching implementation. The caller can override via the optional
 * argument — useful for tests, DI, and the rare case where an admin
 * surface needs to A/B providers in-process.
 *
 * The factory is the only place that touches env vars; callers pass the
 * resulting `WebSearchProvider` around as a value, which keeps `fetchUrl`
 * call sites pure and trivially mockable.
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
}

const KNOWN_PROVIDERS: ReadonlyArray<WebSearchProviderName> = [
  'tavily',
  'firecrawl',
]

function resolveProviderName(
  explicit?: WebSearchProviderName,
): WebSearchProviderName {
  const value = explicit ?? process.env.WEBSEARCH_PROVIDER ?? 'tavily'
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
      const apiKey = options?.apiKey ?? process.env.FIRECRAWL_API_KEY ?? ''
      if (!apiKey) {
        throw new WebSearchProviderError(
          'firecrawl',
          'API key is required (set FIRECRAWL_API_KEY)',
        )
      }
      return createFirecrawlProvider({ apiKey })
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
