import { describe, it, expect, vi } from 'vitest'
import { checkPremium } from '@/lib/subscription/gate'

describe('checkPremium', () => {
  it('returns true when user has active premium subscription', async () => {
    const mockSupabase = {
      from: vi.fn().mockReturnValue({
        select: vi.fn().mockReturnValue({
          eq: vi.fn().mockReturnValue({
            eq: vi.fn().mockReturnValue({
              maybeSingle: vi.fn().mockResolvedValue({
                data: { plan: 'premium', status: 'active' },
              }),
            }),
          }),
        }),
      }),
    }

    const result = await checkPremium(mockSupabase as any, 'user-123')
    expect(result).toBe(true)
  })

  it('returns false when user has free plan', async () => {
    const mockSupabase = {
      from: vi.fn().mockReturnValue({
        select: vi.fn().mockReturnValue({
          eq: vi.fn().mockReturnValue({
            eq: vi.fn().mockReturnValue({
              maybeSingle: vi.fn().mockResolvedValue({
                data: { plan: 'free', status: 'active' },
              }),
            }),
          }),
        }),
      }),
    }

    const result = await checkPremium(mockSupabase as any, 'user-123')
    expect(result).toBe(false)
  })

  it('returns false when no subscription exists', async () => {
    const mockSupabase = {
      from: vi.fn().mockReturnValue({
        select: vi.fn().mockReturnValue({
          eq: vi.fn().mockReturnValue({
            eq: vi.fn().mockReturnValue({
              maybeSingle: vi.fn().mockResolvedValue({
                data: null,
              }),
            }),
          }),
        }),
      }),
    }

    const result = await checkPremium(mockSupabase as any, 'user-123')
    expect(result).toBe(false)
  })

  it('returns false when subscription status is not active', async () => {
    const mockSupabase = {
      from: vi.fn().mockReturnValue({
        select: vi.fn().mockReturnValue({
          eq: vi.fn().mockReturnValue({
            eq: vi.fn().mockReturnValue({
              maybeSingle: vi.fn().mockResolvedValue({
                data: { plan: 'premium', status: 'canceled' },
              }),
            }),
          }),
        }),
      }),
    }

    const result = await checkPremium(mockSupabase as any, 'user-123')
    expect(result).toBe(false)
  })
})
