import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock next/navigation
const mockRedirect = vi.fn()
vi.mock('next/navigation', () => ({
  redirect: (url: string) => {
    mockRedirect(url)
    throw new Error(`NEXT_REDIRECT: ${url}`)
  },
}))

// Mock supabase server client
const mockGetUser = vi.fn()
const mockFrom = vi.fn()
const mockSupabase = {
  auth: { getUser: mockGetUser },
  from: mockFrom,
}

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn(() => Promise.resolve(mockSupabase)),
}))

// Mock queries
const mockGetBusinessProfile = vi.fn()
const mockGetSubscription = vi.fn()

vi.mock('@/lib/supabase/queries', () => ({
  getBusinessProfile: (...args: unknown[]) => mockGetBusinessProfile(...args),
  getSubscription: (...args: unknown[]) => mockGetSubscription(...args),
}))

// Import the module under test
import DashboardLayout from '@/app/(dashboard)/layout'

describe('Dashboard Layout', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('redirects to /login when user is not authenticated', async () => {
    mockGetUser.mockResolvedValue({ data: { user: null }, error: null })

    await expect(
      DashboardLayout({ children: <div>child</div> })
    ).rejects.toThrow('NEXT_REDIRECT: /login')

    expect(mockRedirect).toHaveBeenCalledWith('/login')
  })

  it('redirects to /dashboard/onboarding when no business profile exists', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-123' } },
      error: null,
    })
    mockGetBusinessProfile.mockResolvedValue({ data: null, error: null })
    mockGetSubscription.mockResolvedValue({
      data: { id: 'sub-1', plan: 'free', status: 'active' },
      error: null,
    })

    await expect(
      DashboardLayout({ children: <div>child</div> })
    ).rejects.toThrow('NEXT_REDIRECT: /dashboard/onboarding')

    expect(mockRedirect).toHaveBeenCalledWith('/dashboard/onboarding')
  })

  it('calls getBusinessProfile and getSubscription with correct args', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-456' } },
      error: null,
    })
    mockGetBusinessProfile.mockResolvedValue({
      data: { id: 'profile-1', company_name: 'Test Co' },
      error: null,
    })
    mockGetSubscription.mockResolvedValue({
      data: { id: 'sub-1', plan: 'free', status: 'active' },
      error: null,
    })

    await DashboardLayout({ children: <div>child</div> })

    expect(mockGetBusinessProfile).toHaveBeenCalledWith(mockSupabase, 'user-456')
    expect(mockGetSubscription).toHaveBeenCalledWith(mockSupabase, 'user-456')
  })

  it('renders children and sidebar when authenticated with profile', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-789' } },
      error: null,
    })
    mockGetBusinessProfile.mockResolvedValue({
      data: { id: 'profile-1', company_name: 'Test Co' },
      error: null,
    })
    mockGetSubscription.mockResolvedValue({
      data: { id: 'sub-1', plan: 'free', status: 'active' },
      error: null,
    })

    const result = await DashboardLayout({
      children: <div>child content</div>,
    })

    // The result should be a valid React element (not a redirect)
    expect(result).toBeTruthy()
  })
})
