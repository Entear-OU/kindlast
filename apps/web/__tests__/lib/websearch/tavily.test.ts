import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createTavilyProvider } from '@/lib/websearch/tavily'
import { WebSearchProviderError } from '@/lib/websearch/types'

/**
 * Unit coverage for the Tavily provider (ENT-98).
 *
 * Tavily's Extract API accepts `urls` and returns `{ results: [{ url,
 * raw_content, ... }], failed_results: [...], response_time }`. We mock
 * `globalThis.fetch` so the suite never makes a real HTTPS call —
 * deterministic, no API-key dependency, no network flakiness in CI.
 *
 * The real-Tavily call lives in `tests/integration/websearch-tavily.test.ts`
 * and is `skipif`ed when `TAVILY_API_KEY` is unset.
 */

const SAMPLE_RESPONSE = {
  results: [
    {
      url: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj',
      raw_content:
        'Article 6 — Lawfulness of processing\n\nProcessing shall be lawful…',
      images: [],
    },
  ],
  failed_results: [],
  response_time: 0.42,
}

function mockFetchOnce(response: unknown, init: Partial<Response> = {}) {
  const fetchMock = vi.fn().mockResolvedValueOnce(
    new Response(JSON.stringify(response), {
      status: init.status ?? 200,
      headers: { 'content-type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('createTavilyProvider', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('throws at construction if no API key is supplied (fail loud, not silently no-op)', () => {
    expect(() => createTavilyProvider({ apiKey: '' })).toThrow(/api key/i)
  })

  it('exposes provider.name === "tavily"', () => {
    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })
    expect(provider.name).toBe('tavily')
  })

  it('POSTs to the Extract endpoint with the URL and API key', async () => {
    const fetchMock = mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })

    await provider.fetchUrl('https://eur-lex.europa.eu/eli/reg/2016/679/oj')

    expect(fetchMock).toHaveBeenCalledOnce()
    const [calledUrl, init] = fetchMock.mock.calls[0]!
    expect(calledUrl).toMatch(/api\.tavily\.com.*extract/)
    expect((init as RequestInit).method).toBe('POST')

    const headers = (init as RequestInit).headers as Record<string, string>
    expect(headers['Content-Type']).toBe('application/json')
    // Tavily accepts the key either as `Authorization: Bearer …` or in the
    // request body. We use the Authorization header — fewer keystrokes per
    // call and consistent with most Tavily SDKs.
    expect(headers.Authorization).toBe('Bearer tvly-test-key')

    const body = JSON.parse((init as RequestInit).body as string)
    expect(body.urls).toEqual(['https://eur-lex.europa.eu/eli/reg/2016/679/oj'])
  })

  it('returns a FetchUrlResult with content, url, title, fetchedAt, provider', async () => {
    mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })

    const result = await provider.fetchUrl(
      'https://eur-lex.europa.eu/eli/reg/2016/679/oj',
    )

    expect(result.provider).toBe('tavily')
    expect(result.url).toBe('https://eur-lex.europa.eu/eli/reg/2016/679/oj')
    expect(result.content).toContain('Lawfulness of processing')
    expect(result.fetchedAt).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    // Tavily's response shape does not include a title field; the provider
    // returns null rather than fabricating one.
    expect(result.title).toBeNull()
  })

  it('throws WebSearchProviderError when Tavily returns a non-2xx response', async () => {
    mockFetchOnce({ error: 'rate limited' }, { status: 429 })
    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })

    await expect(
      provider.fetchUrl('https://eur-lex.europa.eu/eli/reg/2016/679/oj'),
    ).rejects.toThrow(WebSearchProviderError)
  })

  it('throws WebSearchProviderError when Tavily reports the URL in failed_results', async () => {
    mockFetchOnce({
      results: [],
      failed_results: [
        {
          url: 'https://example.invalid/missing',
          error: '404 not found',
        },
      ],
      response_time: 0.1,
    })
    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })

    await expect(
      provider.fetchUrl('https://example.invalid/missing'),
    ).rejects.toThrow(/failed.*404/i)
  })

  it('throws WebSearchProviderError when Tavily returns no result for the requested URL', async () => {
    mockFetchOnce({ results: [], failed_results: [], response_time: 0.1 })
    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })

    await expect(
      provider.fetchUrl('https://eur-lex.europa.eu/eli/reg/2016/679/oj'),
    ).rejects.toThrow(/no result/i)
  })

  it('aborts the fetch after the configured timeout', async () => {
    // We can't easily mock real timeouts in the fake fetch, but we can verify
    // the AbortController signal is passed through to fetch so a timeout
    // path exists. The fetch mock receives `signal` in the init object.
    const fetchMock = vi.fn().mockImplementation((_url, init: RequestInit) => {
      expect(init.signal).toBeDefined()
      return Promise.resolve(
        new Response(JSON.stringify(SAMPLE_RESPONSE), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      )
    })
    vi.stubGlobal('fetch', fetchMock)

    const provider = createTavilyProvider({ apiKey: 'tvly-test-key' })
    await provider.fetchUrl('https://eur-lex.europa.eu/eli/reg/2016/679/oj', {
      timeoutMs: 100,
    })
    expect(fetchMock).toHaveBeenCalledOnce()
  })
})
