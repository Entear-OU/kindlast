import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock next/headers
vi.mock('next/headers', () => ({
  cookies: vi.fn(() => ({
    getAll: vi.fn(() => []),
    set: vi.fn(),
  })),
}))

const mockExchangeCodeForSession = vi.fn()

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn(() =>
    Promise.resolve({
      auth: {
        exchangeCodeForSession: mockExchangeCodeForSession,
      },
    })
  ),
}))

// Mock NextResponse.redirect
vi.mock('next/server', () => ({
  NextResponse: {
    redirect: vi.fn((url: URL) => ({
      status: 302,
      headers: new Headers({ Location: url.toString() }),
      url: url.toString(),
    })),
  },
}))

describe('Auth Callback Route', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('exchanges code for session and redirects to /onboarding', async () => {
    const { GET } = await import('@/app/auth/callback/route')

    mockExchangeCodeForSession.mockResolvedValue({
      data: { session: { access_token: 'token' } },
      error: null,
    })

    const request = new Request('http://localhost:3000/auth/callback?code=test-code')
    const response = await GET(request)

    expect(mockExchangeCodeForSession).toHaveBeenCalledWith('test-code')
    expect(response.url).toContain('/onboarding')
  })

  it('redirects to /login when no code is provided', async () => {
    const { GET } = await import('@/app/auth/callback/route')

    const request = new Request('http://localhost:3000/auth/callback')
    const response = await GET(request)

    expect(mockExchangeCodeForSession).not.toHaveBeenCalled()
    expect(response.url).toContain('/login')
  })

  it('redirects to /login on exchange error', async () => {
    const { GET } = await import('@/app/auth/callback/route')

    mockExchangeCodeForSession.mockResolvedValue({
      data: { session: null },
      error: { message: 'Invalid code' },
    })

    const request = new Request('http://localhost:3000/auth/callback?code=bad-code')
    const response = await GET(request)

    expect(response.url).toContain('/login')
  })
})
