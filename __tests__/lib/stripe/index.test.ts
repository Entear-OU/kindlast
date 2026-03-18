import { describe, it, expect, vi, beforeEach } from 'vitest'

// Use vi.hoisted so mocks are available during vi.mock hoisting
const { mockCheckoutSessionsCreate, mockBillingPortalSessionsCreate } = vi.hoisted(() => ({
  mockCheckoutSessionsCreate: vi.fn(),
  mockBillingPortalSessionsCreate: vi.fn(),
}))

vi.mock('stripe', () => {
  return {
    default: vi.fn().mockImplementation(function () {
      return {
        checkout: {
          sessions: {
            create: mockCheckoutSessionsCreate,
          },
        },
        billingPortal: {
          sessions: {
            create: mockBillingPortalSessionsCreate,
          },
        },
      }
    }),
  }
})

// Set env vars before importing
vi.stubEnv('STRIPE_SECRET_KEY', 'sk_test_123')
vi.stubEnv('STRIPE_PRICE_ID_PREMIUM', 'price_premium_123')
vi.stubEnv('NEXT_PUBLIC_APP_URL', 'https://kindlast.com')

import { createCheckoutSession, createCustomerPortalSession } from '@/lib/stripe'

describe('Stripe utilities', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('createCheckoutSession', () => {
    it('creates a checkout session with correct params', async () => {
      const mockSession = {
        id: 'cs_test_123',
        url: 'https://checkout.stripe.com/session_123',
      }
      mockCheckoutSessionsCreate.mockResolvedValue(mockSession)

      const result = await createCheckoutSession('user-123', 'test@example.com')

      expect(mockCheckoutSessionsCreate).toHaveBeenCalledWith({
        customer_email: 'test@example.com',
        mode: 'subscription',
        line_items: [
          {
            price: 'price_premium_123',
            quantity: 1,
          },
        ],
        success_url: 'https://kindlast.com/dashboard?upgraded=true',
        cancel_url: 'https://kindlast.com/pricing',
        metadata: {
          user_id: 'user-123',
        },
      })
      expect(result).toEqual(mockSession)
    })
  })

  describe('createCustomerPortalSession', () => {
    it('creates a portal session with correct params', async () => {
      const mockSession = {
        id: 'bps_test_123',
        url: 'https://billing.stripe.com/session_123',
      }
      mockBillingPortalSessionsCreate.mockResolvedValue(mockSession)

      const result = await createCustomerPortalSession('cus_123')

      expect(mockBillingPortalSessionsCreate).toHaveBeenCalledWith({
        customer: 'cus_123',
        return_url: 'https://kindlast.com/dashboard/settings',
      })
      expect(result).toEqual(mockSession)
    })
  })
})
