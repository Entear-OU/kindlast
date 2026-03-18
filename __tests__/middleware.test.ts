import { describe, it, expect, vi, beforeEach } from 'vitest'
import { NextRequest } from 'next/server'

// Mock the supabase middleware helper
const mockUpdateSession = vi.fn()

vi.mock('@/lib/supabase/middleware', () => ({
  updateSession: mockUpdateSession,
}))

// We need to mock NextResponse properly
vi.mock('next/server', async () => {
  const actual = await vi.importActual('next/server')
  return {
    ...actual,
    NextResponse: {
      next: vi.fn(() => ({
        cookies: { set: vi.fn() },
        headers: new Headers(),
      })),
      redirect: vi.fn((url: URL) => ({
        status: 302,
        headers: new Headers({ Location: url.toString() }),
        cookies: { set: vi.fn() },
        url: url.toString(),
      })),
    },
  }
})

function createRequest(url: string) {
  return new NextRequest(new URL(url, 'http://localhost:3000'))
}

describe('Middleware', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.resetModules()
  })

  it('redirects unauthenticated users from /dashboard to /login', async () => {
    const { middleware } = await import('@/middleware')
    const { NextResponse } = await import('next/server')

    const supabaseResponse = {
      cookies: { set: vi.fn() },
      headers: new Headers(),
    }
    mockUpdateSession.mockResolvedValue({ supabaseResponse, user: null })

    const request = createRequest('/dashboard')
    await middleware(request)

    expect(NextResponse.redirect).toHaveBeenCalled()
    const redirectCall = vi.mocked(NextResponse.redirect).mock.calls[0]
    expect(redirectCall[0].toString()).toContain('/login')
  })

  it('redirects authenticated users from /login to /dashboard', async () => {
    const { middleware } = await import('@/middleware')
    const { NextResponse } = await import('next/server')

    const supabaseResponse = {
      cookies: { set: vi.fn() },
      headers: new Headers(),
    }
    mockUpdateSession.mockResolvedValue({
      supabaseResponse,
      user: { id: '123', email: 'test@example.com' },
    })

    const request = createRequest('/login')
    await middleware(request)

    expect(NextResponse.redirect).toHaveBeenCalled()
    const redirectCall = vi.mocked(NextResponse.redirect).mock.calls[0]
    expect(redirectCall[0].toString()).toContain('/dashboard')
  })

  it('allows authenticated users to access /dashboard', async () => {
    const { middleware } = await import('@/middleware')
    const { NextResponse } = await import('next/server')

    const supabaseResponse = {
      cookies: { set: vi.fn() },
      headers: new Headers(),
    }
    mockUpdateSession.mockResolvedValue({
      supabaseResponse,
      user: { id: '123', email: 'test@example.com' },
    })

    const request = createRequest('/dashboard')
    const response = await middleware(request)

    expect(NextResponse.redirect).not.toHaveBeenCalled()
    expect(response).toBe(supabaseResponse)
  })

  it('allows unauthenticated users to access /login', async () => {
    const { middleware } = await import('@/middleware')
    const { NextResponse } = await import('next/server')

    const supabaseResponse = {
      cookies: { set: vi.fn() },
      headers: new Headers(),
    }
    mockUpdateSession.mockResolvedValue({ supabaseResponse, user: null })

    const request = createRequest('/login')
    const response = await middleware(request)

    expect(NextResponse.redirect).not.toHaveBeenCalled()
    expect(response).toBe(supabaseResponse)
  })

  it('passes through non-protected routes', async () => {
    const { middleware } = await import('@/middleware')
    const { NextResponse } = await import('next/server')

    const supabaseResponse = {
      cookies: { set: vi.fn() },
      headers: new Headers(),
    }
    mockUpdateSession.mockResolvedValue({ supabaseResponse, user: null })

    const request = createRequest('/pricing')
    const response = await middleware(request)

    expect(NextResponse.redirect).not.toHaveBeenCalled()
    expect(response).toBe(supabaseResponse)
  })
})
