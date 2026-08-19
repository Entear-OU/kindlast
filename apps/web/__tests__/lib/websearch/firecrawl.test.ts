import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createFirecrawlProvider } from '@/lib/websearch/firecrawl'
import { WebSearchProviderError } from '@/lib/websearch/types'

/**
 * Unit coverage for the Firecrawl provider (ENT-98, implemented in ENT-240).
 *
 * Firecrawl's scrape API is `POST {base}/v2/scrape` with `{ url, formats }`
 * and returns `{ success, data: { markdown, metadata: { title, sourceURL,
 * statusCode } } }`. `globalThis.fetch` is stubbed throughout, so this suite
 * never opens a socket: it needs no key, no self-hosted instance and no
 * network, and it is deterministic in CI.
 *
 * WHAT THIS SUITE DOES NOT PROVE. It pins the request this provider builds and
 * the way it reads a response, both against the published API shape. It has
 * never been run against a real Firecrawl instance, hosted or self-hosted, so
 * a mismatch between the documented shape and a running build would pass here
 * and fail there. That is stated rather than implied because nothing in the
 * product calls this module yet, so there is no end-to-end path that would
 * catch it either.
 */

const SAMPLE_RESPONSE = {
  success: true,
  data: {
    markdown:
      '# Article 6\n\nLawfulness of processing\n\nProcessing shall be lawful only if...',
    metadata: {
      title: 'Regulation (EU) 2016/679',
      sourceURL: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj',
      statusCode: 200,
    },
  },
}

const URL_UNDER_TEST = 'https://eur-lex.europa.eu/eli/reg/2016/679/oj'

function mockFetchOnce(response: unknown, init: { status?: number } = {}) {
  const fetchMock = vi.fn().mockResolvedValueOnce(
    new Response(JSON.stringify(response), {
      status: init.status ?? 200,
      headers: { 'content-type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function initOf(fetchMock: ReturnType<typeof vi.fn>): RequestInit {
  return fetchMock.mock.calls[0]![1] as RequestInit
}

function headersOf(
  fetchMock: ReturnType<typeof vi.fn>,
): Record<string, string> {
  return initOf(fetchMock).headers as Record<string, string>
}

function bodyOf(fetchMock: ReturnType<typeof vi.fn>): Record<string, unknown> {
  return JSON.parse(initOf(fetchMock).body as string) as Record<string, unknown>
}

describe('createFirecrawlProvider', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exposes provider.name === "firecrawl"', () => {
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })
    expect(provider.name).toBe('firecrawl')
  })

  it('constructs with no API key when a self-hosted base URL is given', () => {
    // The self-hosted image runs with USE_DB_AUTHENTICATION=false and accepts
    // unauthenticated requests. Demanding a key there would make the one
    // provider that can run air-gapped impossible to configure air-gapped.
    expect(() =>
      createFirecrawlProvider({ baseUrl: 'http://firecrawl:3002' }),
    ).not.toThrow()
  })

  it('throws at construction when neither an API key nor a base URL is given', () => {
    // Falling back to the hosted API with no credential would fail at the
    // first call with a 401, one layer further from the mistake.
    expect(() => createFirecrawlProvider({})).toThrow(WebSearchProviderError)
    expect(() => createFirecrawlProvider({})).toThrow(/FIRECRAWL_API_URL/)
  })

  it('POSTs to {base}/v2/scrape asking for markdown', async () => {
    const fetchMock = mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    await provider.fetchUrl(URL_UNDER_TEST)

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls[0]![0]).toBe('http://firecrawl:3002/v2/scrape')
    expect(initOf(fetchMock).method).toBe('POST')
    expect(headersOf(fetchMock)['Content-Type']).toBe('application/json')
    expect(bodyOf(fetchMock)).toMatchObject({
      url: URL_UNDER_TEST,
      formats: ['markdown'],
      onlyMainContent: true,
    })
  })

  it('trims a trailing slash off the base URL rather than double-slashing the path', async () => {
    const fetchMock = mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002/',
    })

    await provider.fetchUrl(URL_UNDER_TEST)

    expect(fetchMock.mock.calls[0]![0]).toBe('http://firecrawl:3002/v2/scrape')
  })

  it('defaults to the hosted API when only a key is given', async () => {
    const fetchMock = mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createFirecrawlProvider({ apiKey: 'fc-test-key' })

    await provider.fetchUrl(URL_UNDER_TEST)

    expect(fetchMock.mock.calls[0]![0]).toBe(
      'https://api.firecrawl.dev/v2/scrape',
    )
    expect(headersOf(fetchMock).Authorization).toBe('Bearer fc-test-key')
  })

  it('sends no Authorization header when a self-hosted instance needs no key', async () => {
    const fetchMock = mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    await provider.fetchUrl(URL_UNDER_TEST)

    expect(headersOf(fetchMock).Authorization).toBeUndefined()
  })

  it('sends the key to a self-hosted instance that was configured with one', async () => {
    const fetchMock = mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
      apiKey: 'fc-self-hosted-key',
    })

    await provider.fetchUrl(URL_UNDER_TEST)

    expect(headersOf(fetchMock).Authorization).toBe('Bearer fc-self-hosted-key')
  })

  it('returns a FetchUrlResult with content, url, title, fetchedAt, provider', async () => {
    mockFetchOnce(SAMPLE_RESPONSE)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    const result = await provider.fetchUrl(URL_UNDER_TEST)

    expect(result.provider).toBe('firecrawl')
    expect(result.url).toBe(URL_UNDER_TEST)
    expect(result.title).toBe('Regulation (EU) 2016/679')
    expect(result.content).toContain('Lawfulness of processing')
    expect(result.fetchedAt).toMatch(/^\d{4}-\d{2}-\d{2}T/)
  })

  it('prefers the final URL the provider reports over the one asked for', async () => {
    mockFetchOnce({
      success: true,
      data: {
        markdown: 'text',
        metadata: {
          title: 'Consolidated text',
          sourceURL:
            'https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX',
          statusCode: 200,
        },
      },
    })
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    const result = await provider.fetchUrl(URL_UNDER_TEST)

    expect(result.url).toBe(
      'https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX',
    )
  })

  it('takes the first title when the page declares several, and null when it declares none', async () => {
    // `metadata.title` is `string | string[]` in the published response shape,
    // because a page can carry more than one title tag.
    mockFetchOnce({
      success: true,
      data: { markdown: 'text', metadata: { title: ['First', 'Second'] } },
    })
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })
    expect((await provider.fetchUrl(URL_UNDER_TEST)).title).toBe('First')

    mockFetchOnce({ success: true, data: { markdown: 'text', metadata: {} } })
    const second = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })
    expect((await second.fetchUrl(URL_UNDER_TEST)).title).toBeNull()
  })

  it('throws WebSearchProviderError on a non-2xx response', async () => {
    mockFetchOnce({ error: 'Unauthorized' }, { status: 401 })
    const provider = createFirecrawlProvider({ apiKey: 'fc-test-key' })

    await expect(provider.fetchUrl(URL_UNDER_TEST)).rejects.toThrow(
      WebSearchProviderError,
    )

    mockFetchOnce({ error: 'Unauthorized' }, { status: 401 })
    await expect(
      createFirecrawlProvider({ apiKey: 'fc-test-key' }).fetchUrl(
        URL_UNDER_TEST,
      ),
    ).rejects.toThrow(/401/)
  })

  it('throws when the body says success: false, even with HTTP 200', async () => {
    // Firecrawl answers 200 with `success: false` for a page it could not
    // scrape. Reading only the status here would hand the caller an empty
    // citation, which is the worst outcome for a surface whose value is that
    // a human can check the claim.
    mockFetchOnce({ success: false, error: 'site returned 403' })
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    await expect(provider.fetchUrl(URL_UNDER_TEST)).rejects.toThrow(
      /site returned 403/,
    )
  })

  it('throws when the response carries no markdown', async () => {
    mockFetchOnce({ success: true, data: { metadata: { statusCode: 200 } } })
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    await expect(provider.fetchUrl(URL_UNDER_TEST)).rejects.toThrow(
      /no content/i,
    )
  })

  it('throws on a malformed JSON body', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response('<html>gateway timeout</html>', {
        status: 200,
        headers: { 'content-type': 'text/html' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    await expect(provider.fetchUrl(URL_UNDER_TEST)).rejects.toThrow(
      /malformed JSON/i,
    )
  })

  it('wraps a network error rather than letting the raw failure escape', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('ECONNREFUSED'))
    vi.stubGlobal('fetch', fetchMock)
    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })

    await expect(provider.fetchUrl(URL_UNDER_TEST)).rejects.toThrow(
      WebSearchProviderError,
    )
    await expect(provider.fetchUrl(URL_UNDER_TEST)).rejects.toThrow(
      /network error/i,
    )
  })

  it('passes an abort signal so the call is bounded', async () => {
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

    const provider = createFirecrawlProvider({
      baseUrl: 'http://firecrawl:3002',
    })
    await provider.fetchUrl(URL_UNDER_TEST, { timeoutMs: 100 })

    expect(fetchMock).toHaveBeenCalledOnce()
  })
})
