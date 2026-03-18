import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock next/cache
vi.mock('next/cache', () => ({
  revalidatePath: vi.fn(),
}))

// Mock next/navigation
vi.mock('next/navigation', () => ({
  redirect: vi.fn(),
}))

// Mock Supabase
const mockSupabase = {
  auth: {
    getUser: vi.fn(),
  },
  from: vi.fn(),
}

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn().mockResolvedValue(mockSupabase),
}))

describe('onboarding actions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('saveBusinessProfile', () => {
    it('throws error when user is not authenticated', async () => {
      mockSupabase.auth.getUser.mockResolvedValue({
        data: { user: null },
      })

      const { saveBusinessProfile } = await import(
        '@/app/(dashboard)/dashboard/onboarding/actions'
      )

      await expect(
        saveBusinessProfile({
          company_name: 'Test',
          country: 'Estonia',
          processes_personal_data: true,
          data_types: [],
          third_party_processors: [],
          transfers_data_outside_eu: false,
          has_privacy_policy: false,
          has_cookie_consent: false,
          has_dpo: false,
          has_breach_notification: false,
          has_dsr_process: false,
          uses_ai_systems: false,
        })
      ).rejects.toThrow('Unauthorized')
    })

    it('upserts business profile with correct data', async () => {
      const mockUser = { id: 'user-123' }
      mockSupabase.auth.getUser.mockResolvedValue({
        data: { user: mockUser },
      })

      const mockSelect = vi.fn().mockReturnValue({
        single: vi.fn().mockResolvedValue({
          data: { id: 'profile-123', user_id: 'user-123', company_name: 'Test' },
          error: null,
        }),
      })
      const mockUpsert = vi.fn().mockReturnValue({ select: mockSelect })
      mockSupabase.from.mockReturnValue({ upsert: mockUpsert })

      const { saveBusinessProfile } = await import(
        '@/app/(dashboard)/dashboard/onboarding/actions'
      )

      const profileData = {
        company_name: 'Test Company',
        country: 'Estonia',
        processes_personal_data: true,
        data_types: ['email'],
        third_party_processors: ['Stripe'],
        transfers_data_outside_eu: false,
        has_privacy_policy: true,
        has_cookie_consent: false,
        has_dpo: false,
        has_breach_notification: false,
        has_dsr_process: false,
        uses_ai_systems: false,
      }

      await saveBusinessProfile(profileData)

      expect(mockSupabase.from).toHaveBeenCalledWith('business_profiles')
      expect(mockUpsert).toHaveBeenCalledWith(
        expect.objectContaining({
          ...profileData,
          user_id: 'user-123',
        }),
        expect.objectContaining({ onConflict: 'user_id' })
      )
    })
  })

  describe('completeOnboarding', () => {
    it('throws error when user is not authenticated', async () => {
      mockSupabase.auth.getUser.mockResolvedValue({
        data: { user: null },
      })

      const { completeOnboarding } = await import(
        '@/app/(dashboard)/dashboard/onboarding/actions'
      )

      await expect(completeOnboarding()).rejects.toThrow('Unauthorized')
    })

    it('redirects to dashboard when authenticated', async () => {
      const mockUser = { id: 'user-123' }
      mockSupabase.auth.getUser.mockResolvedValue({
        data: { user: mockUser },
      })

      // Mock the profile query chain
      mockSupabase.from.mockReturnValue({
        select: vi.fn().mockReturnValue({
          eq: vi.fn().mockReturnValue({
            single: vi.fn().mockResolvedValue({
              data: { id: 'profile-1' },
              error: null,
            }),
          }),
        }),
        insert: vi.fn().mockReturnValue({
          select: vi.fn().mockReturnValue({
            single: vi.fn().mockResolvedValue({
              data: { id: 'assessment-1' },
              error: null,
            }),
          }),
        }),
      })

      // Mock fetch for background assessment trigger
      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true })

      const { redirect } = await import('next/navigation')
      const { completeOnboarding } = await import(
        '@/app/(dashboard)/dashboard/onboarding/actions'
      )

      await completeOnboarding()

      expect(redirect).toHaveBeenCalledWith('/dashboard')
    })
  })
})
