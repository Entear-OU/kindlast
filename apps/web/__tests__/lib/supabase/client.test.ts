import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockCreateBrowserClient = vi.fn().mockReturnValue({ auth: {} })

vi.mock('@supabase/ssr', () => ({
  createBrowserClient: mockCreateBrowserClient,
}))

describe('lib/supabase/client', () => {
  beforeEach(() => {
    vi.stubEnv('SUPABASE_URL', 'https://test.supabase.co')
    vi.stubEnv('SUPABASE_PUBLISHABLE_KEY', 'sb_publishable_test')
    mockCreateBrowserClient.mockClear()
  })

  it('exports a createClient function', async () => {
    const { createClient } = await import('@/lib/supabase/client')
    expect(typeof createClient).toBe('function')
  })

  it('calls createBrowserClient with correct env vars', async () => {
    const { createClient } = await import('@/lib/supabase/client')
    createClient()

    expect(mockCreateBrowserClient).toHaveBeenCalledWith(
      'https://test.supabase.co',
      'sb_publishable_test'
    )
  })

  it('returns a supabase client instance', async () => {
    const { createClient } = await import('@/lib/supabase/client')
    const client = createClient()
    expect(client).toBeDefined()
    expect(client).toHaveProperty('auth')
  })
})
