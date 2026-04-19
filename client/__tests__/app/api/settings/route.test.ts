import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockGetUser = vi.fn()
const mockFrom = vi.fn()

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn().mockResolvedValue({
    auth: {
      getUser: () => mockGetUser(),
    },
    from: (...args: unknown[]) => mockFrom(...args),
  }),
}))

describe('GET /api/settings', () => {
  const mockProfile = {
    id: 'profile-1',
    user_id: 'user-1',
    company_name: 'Test Corp',
    country: 'Estonia',
    industry: 'Technology',
    employee_count: 50,
    processes_personal_data: true,
    data_types: ['email', 'name'],
    uses_ai_systems: false,
    ai_system_descriptions: null,
    third_party_processors: ['AWS', 'Google'],
    transfers_data_outside_eu: false,
    has_dpo: true,
    has_privacy_policy: true,
    has_cookie_consent: true,
    has_breach_notification: false,
    has_dsr_process: true,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  }

  const mockSubscription = {
    id: 'sub-1',
    user_id: 'user-1',
    stripe_customer_id: 'cus_123',
    stripe_subscription_id: 'sub_123',
    plan: 'premium',
    status: 'active',
    current_period_end: '2024-12-31T00:00:00Z',
    created_at: '2024-01-01T00:00:00Z',
  }

  function setupMocks(options: {
    profile?: typeof mockProfile | null
    profileError?: Error | null
    subscription?: typeof mockSubscription | null
    subscriptionError?: Error | null
  } = {}) {
    const profileChain = {
      select: vi.fn().mockReturnValue({
        eq: vi.fn().mockReturnValue({
          maybeSingle: vi.fn().mockResolvedValue({
            data: options.profile === undefined ? mockProfile : options.profile,
            error: options.profileError ?? null,
          }),
        }),
      }),
    }

    const subscriptionChain = {
      select: vi.fn().mockReturnValue({
        eq: vi.fn().mockReturnValue({
          maybeSingle: vi.fn().mockResolvedValue({
            data: options.subscription === undefined ? mockSubscription : options.subscription,
            error: options.subscriptionError ?? null,
          }),
        }),
      }),
    }

    mockFrom.mockImplementation((table: string) => {
      if (table === 'business_profiles') return profileChain
      if (table === 'subscriptions') return subscriptionChain
      return {}
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.resetModules()

    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
    })
  })

  it('returns 401 when not authenticated', async () => {
    mockGetUser.mockResolvedValue({ data: { user: null } })
    setupMocks()

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(401)

    const data = await response.json()
    expect(data.error).toBe('Unauthorized')
  })

  it('returns profile and subscription for authenticated user', async () => {
    setupMocks()

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(200)

    const data = await response.json()
    expect(data.profile).toEqual(mockProfile)
    expect(data.subscription).toEqual(mockSubscription)
  })

  it('handles missing profile gracefully', async () => {
    setupMocks({ profile: null })

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(200)

    const data = await response.json()
    expect(data.profile).toBeNull()
    expect(data.subscription).toEqual(mockSubscription)
  })

  it('handles missing subscription gracefully', async () => {
    setupMocks({ subscription: null })

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(200)

    const data = await response.json()
    expect(data.profile).toEqual(mockProfile)
    expect(data.subscription).toBeNull()
  })

  it('handles both profile and subscription missing', async () => {
    setupMocks({ profile: null, subscription: null })

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(200)

    const data = await response.json()
    expect(data.profile).toBeNull()
    expect(data.subscription).toBeNull()
  })

  it('queries supabase with correct user id', async () => {
    setupMocks()

    const { GET } = await import('@/app/api/settings/route')

    await GET()

    expect(mockFrom).toHaveBeenCalledWith('business_profiles')
    expect(mockFrom).toHaveBeenCalledWith('subscriptions')
  })

  it('returns 500 on profile database error', async () => {
    setupMocks({ profileError: new Error('Database connection failed') })

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(500)

    const data = await response.json()
    expect(data.error).toBe('Failed to fetch profile')
  })

  it('returns 500 on subscription database error', async () => {
    setupMocks({ subscriptionError: new Error('Database connection failed') })

    const { GET } = await import('@/app/api/settings/route')

    const response = await GET()
    expect(response.status).toBe(500)

    const data = await response.json()
    expect(data.error).toBe('Failed to fetch subscription')
  })
})
