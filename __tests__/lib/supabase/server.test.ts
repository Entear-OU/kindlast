import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockCreateServerClient = vi.fn().mockReturnValue({ auth: {} })

vi.mock('@supabase/ssr', () => ({
  createServerClient: mockCreateServerClient,
}))

const mockGet = vi.fn().mockReturnValue({ value: 'test-cookie-value' })
const mockSet = vi.fn()
const mockCookieStore = {
  getAll: vi.fn().mockReturnValue([{ name: 'test', value: 'value' }]),
  set: mockSet,
}

vi.mock('next/headers', () => ({
  cookies: vi.fn().mockResolvedValue(mockCookieStore),
}))

describe('lib/supabase/server', () => {
  beforeEach(() => {
    vi.stubEnv('NEXT_PUBLIC_SUPABASE_URL', 'https://test.supabase.co')
    vi.stubEnv('NEXT_PUBLIC_SUPABASE_ANON_KEY', 'test-anon-key')
    mockCreateServerClient.mockClear()
    mockCookieStore.getAll.mockClear()
    mockCookieStore.set.mockClear()
  })

  it('exports a createClient function', async () => {
    const { createClient } = await import('@/lib/supabase/server')
    expect(typeof createClient).toBe('function')
  })

  it('calls createServerClient with correct env vars and cookie handlers', async () => {
    const { createClient } = await import('@/lib/supabase/server')
    await createClient()

    expect(mockCreateServerClient).toHaveBeenCalledWith(
      'https://test.supabase.co',
      'test-anon-key',
      expect.objectContaining({
        cookies: expect.objectContaining({
          getAll: expect.any(Function),
          setAll: expect.any(Function),
        }),
      })
    )
  })

  it('returns a supabase client instance', async () => {
    const { createClient } = await import('@/lib/supabase/server')
    const client = await createClient()
    expect(client).toBeDefined()
    expect(client).toHaveProperty('auth')
  })
})
