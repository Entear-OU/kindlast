import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock next/headers
vi.mock('next/headers', () => ({
  cookies: vi.fn(() => ({
    getAll: vi.fn().mockReturnValue([]),
    set: vi.fn(),
  })),
}))

// Mock Supabase server client
const mockGetUser = vi.fn()
const mockSelect = vi.fn()
const mockEq = vi.fn()
const mockMaybeSingle = vi.fn()

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn(() => ({
    auth: {
      getUser: mockGetUser,
    },
    from: vi.fn(() => ({
      select: mockSelect.mockReturnValue({
        eq: mockEq.mockReturnValue({
          maybeSingle: mockMaybeSingle,
        }),
      }),
    })),
  })),
}))

// Mock Stripe functions
const mockCreateCheckoutSession = vi.fn()
const mockCreateCustomerPortalSession = vi.fn()

vi.mock('@/lib/stripe', () => ({
  createCheckoutSession: (...args: unknown[]) => mockCreateCheckoutSession(...args),
  createCustomerPortalSession: (...args: unknown[]) => mockCreateCustomerPortalSession(...args),
}))

import { createCheckout, createPortalSession } from '@/lib/stripe/actions'

describe('Stripe server actions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('createCheckout', () => {
    it('creates a checkout session and returns URL', async () => {
      mockGetUser.mockResolvedValue({
        data: { user: { id: 'user-123', email: 'test@example.com' } },
      })
      mockCreateCheckoutSession.mockResolvedValue({
        url: 'https://checkout.stripe.com/session_123',
      })

      const result = await createCheckout()

      expect(mockCreateCheckoutSession).toHaveBeenCalledWith('user-123', 'test@example.com')
      expect(result).toEqual({ url: 'https://checkout.stripe.com/session_123' })
    })

    it('throws error when user is not authenticated', async () => {
      mockGetUser.mockResolvedValue({
        data: { user: null },
      })

      await expect(createCheckout()).rejects.toThrow('Unauthorized')
    })
  })

  describe('createPortalSession', () => {
    it('creates a portal session and returns URL', async () => {
      mockGetUser.mockResolvedValue({
        data: { user: { id: 'user-123' } },
      })
      mockMaybeSingle.mockResolvedValue({
        data: { stripe_customer_id: 'cus_123' },
      })
      mockCreateCustomerPortalSession.mockResolvedValue({
        url: 'https://billing.stripe.com/session_123',
      })

      const result = await createPortalSession()

      expect(mockCreateCustomerPortalSession).toHaveBeenCalledWith('cus_123')
      expect(result).toEqual({ url: 'https://billing.stripe.com/session_123' })
    })

    it('throws error when no subscription found', async () => {
      mockGetUser.mockResolvedValue({
        data: { user: { id: 'user-123' } },
      })
      mockMaybeSingle.mockResolvedValue({
        data: null,
      })

      await expect(createPortalSession()).rejects.toThrow('No subscription found')
    })
  })
})
