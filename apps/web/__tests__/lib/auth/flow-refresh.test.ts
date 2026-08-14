import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * Spending the refresh token, on the back channel.
 *
 * The session outlives the access token deliberately: the cookie is good for
 * thirty days and an access token for minutes or hours (§1.2). Without this
 * call, that difference is not a feature but a bug with a delay on it, because
 * a person who returns the next day holds a session the server accepts and a
 * token core-api refuses.
 *
 * Same back channel as the code exchange, and for the same reason: the client
 * secret travels from this process to the token endpoint and the browser is
 * never involved.
 */

const discoverProvider = vi.fn()

vi.mock('@/lib/auth/oidc', async () => {
  const actual =
    await vi.importActual<typeof import('@/lib/auth/oidc')>('@/lib/auth/oidc')
  return { ...actual, discoverProvider: () => discoverProvider() }
})

const TOKEN_ENDPOINT = 'http://auth.test/oauth/v2/token'

function tokenResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('refreshTokens', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubEnv('KINDLAST_OIDC_CLIENT_ID', 'web-client')
    vi.stubEnv('KINDLAST_OIDC_CLIENT_SECRET', 'web-secret')
    discoverProvider.mockResolvedValue({ tokenEndpoint: TOKEN_ENDPOINT })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it('presents the refresh token and the client credentials', async () => {
    fetchMock.mockResolvedValue(
      tokenResponse({ access_token: 'new-access', expires_in: 3600 }),
    )

    const { refreshTokens } = await import('@/lib/auth/flow')
    await refreshTokens('the-refresh-token')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe(TOKEN_ENDPOINT)
    expect(init.method).toBe('POST')

    const sent = new URLSearchParams(init.body as string)
    expect(sent.get('grant_type')).toBe('refresh_token')
    expect(sent.get('refresh_token')).toBe('the-refresh-token')
    expect(sent.get('client_id')).toBe('web-client')
    expect(sent.get('client_secret')).toBe('web-secret')
  })

  it('returns the rotated refresh token when the server issues one', async () => {
    // Zitadel rotates: the response carries a new refresh token and the old one
    // is spent. Losing it here would strand the session at the next refresh.
    fetchMock.mockResolvedValue(
      tokenResponse({
        access_token: 'new-access',
        refresh_token: 'rotated',
        id_token: 'new-id',
        expires_in: 3600,
      }),
    )

    const { refreshTokens } = await import('@/lib/auth/flow')
    const tokens = await refreshTokens('the-refresh-token')

    expect(tokens.accessToken).toBe('new-access')
    expect(tokens.refreshToken).toBe('rotated')
    expect(tokens.idToken).toBe('new-id')
  })

  it('reports no refresh token when the server does not rotate', async () => {
    // Not every server rotates. Null here means "unchanged" to the caller
    // rather than "gone", and the caller keeps what it already had.
    fetchMock.mockResolvedValue(
      tokenResponse({ access_token: 'new-access', expires_in: 3600 }),
    )

    const { refreshTokens } = await import('@/lib/auth/flow')
    expect((await refreshTokens('the-refresh-token')).refreshToken).toBeNull()
  })

  it('dates the expiry with slack, so the next refresh lands before the 401', async () => {
    fetchMock.mockResolvedValue(
      tokenResponse({ access_token: 'new-access', expires_in: 3600 }),
    )

    const now = Math.floor(Date.now() / 1000)
    const { refreshTokens } = await import('@/lib/auth/flow')
    const tokens = await refreshTokens('the-refresh-token')

    expect(tokens.expiresAt).toBeLessThan(now + 3600)
    expect(tokens.expiresAt).toBeGreaterThan(now + 3500)
  })

  it('throws without leaking the body, which carries client context', async () => {
    fetchMock.mockResolvedValue(tokenResponse({ error: 'invalid_grant' }, 400))

    const { refreshTokens } = await import('@/lib/auth/flow')
    await expect(refreshTokens('spent-token')).rejects.toThrow(/400/)
    await expect(refreshTokens('spent-token')).rejects.not.toThrow(
      /invalid_grant/,
    )
  })

  it('throws when the response carries no access token', async () => {
    fetchMock.mockResolvedValue(tokenResponse({ token_type: 'Bearer' }))

    const { refreshTokens } = await import('@/lib/auth/flow')
    await expect(refreshTokens('the-refresh-token')).rejects.toThrow(
      /access token/i,
    )
  })
})
