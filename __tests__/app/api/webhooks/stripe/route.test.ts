import { describe, it, expect, vi, beforeEach } from 'vitest'

const { mockConstructEvent, mockFrom, mockUpsert, mockUpdate } = vi.hoisted(() => ({
  mockConstructEvent: vi.fn(),
  mockFrom: vi.fn(),
  mockUpsert: vi.fn().mockReturnValue({ error: null }),
  mockUpdate: vi.fn().mockReturnValue({ eq: vi.fn().mockReturnValue({ error: null }) }),
}))

vi.mock('@/lib/stripe', () => ({
  getStripe: vi.fn(() => ({
    webhooks: {
      constructEvent: mockConstructEvent,
    },
  })),
}))

vi.mock('@/lib/supabase/service-role', () => ({
  createServiceRoleClient: vi.fn(() => ({
    from: mockFrom,
  })),
}))

vi.stubEnv('STRIPE_SECRET_KEY', 'sk_test_123')
vi.stubEnv('STRIPE_WEBHOOK_SECRET', 'whsec_test_123')

import { POST } from '@/app/api/webhooks/stripe/route'

describe('Stripe Webhook Route', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFrom.mockReturnValue({
      upsert: mockUpsert,
      update: mockUpdate,
    })
  })

  function createMockRequest(body: string) {
    return new Request('https://kindlast.com/api/webhooks/stripe', {
      method: 'POST',
      body,
      headers: {
        'stripe-signature': 'sig_test_123',
      },
    })
  }

  it('handles checkout.session.completed event', async () => {
    const event = {
      type: 'checkout.session.completed',
      data: {
        object: {
          metadata: { user_id: 'user-123' },
          customer: 'cus_123',
          subscription: 'sub_123',
        },
      },
    }
    mockConstructEvent.mockReturnValue(event)

    const response = await POST(createMockRequest(JSON.stringify(event)))

    expect(response.status).toBe(200)
    expect(mockFrom).toHaveBeenCalledWith('subscriptions')
    expect(mockUpsert).toHaveBeenCalledWith(
      {
        user_id: 'user-123',
        stripe_customer_id: 'cus_123',
        stripe_subscription_id: 'sub_123',
        plan: 'premium',
        status: 'active',
      },
      { onConflict: 'user_id' }
    )
  })

  it('handles customer.subscription.updated event', async () => {
    const event = {
      type: 'customer.subscription.updated',
      data: {
        object: {
          id: 'sub_123',
          status: 'active',
          current_period_end: 1700000000,
          metadata: { user_id: 'user-123' },
        },
      },
    }
    mockConstructEvent.mockReturnValue(event)

    const response = await POST(createMockRequest(JSON.stringify(event)))

    expect(response.status).toBe(200)
    expect(mockFrom).toHaveBeenCalledWith('subscriptions')
  })

  it('handles customer.subscription.deleted event', async () => {
    const event = {
      type: 'customer.subscription.deleted',
      data: {
        object: {
          id: 'sub_123',
          metadata: { user_id: 'user-123' },
        },
      },
    }
    mockConstructEvent.mockReturnValue(event)

    const response = await POST(createMockRequest(JSON.stringify(event)))

    expect(response.status).toBe(200)
    expect(mockFrom).toHaveBeenCalledWith('subscriptions')
  })

  it('returns 400 for invalid signature', async () => {
    mockConstructEvent.mockImplementation(() => {
      throw new Error('Invalid signature')
    })

    const response = await POST(createMockRequest('invalid'))

    expect(response.status).toBe(400)
  })
})
