import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  getBusinessProfile,
  getLatestAssessment,
  getFindings,
  getSubscription,
} from '@/lib/supabase/queries'
import type { SupabaseClient } from '@supabase/supabase-js'

function createMockSupabase() {
  const chain = {
    select: vi.fn().mockReturnThis(),
    eq: vi.fn().mockReturnThis(),
    order: vi.fn().mockReturnThis(),
    limit: vi.fn().mockReturnThis(),
    single: vi.fn().mockResolvedValue({ data: null, error: null }),
    maybeSingle: vi.fn().mockResolvedValue({ data: null, error: null }),
  }

  return {
    from: vi.fn().mockReturnValue(chain),
    _chain: chain,
  } as unknown as SupabaseClient & { _chain: typeof chain }
}

describe('lib/supabase/queries', () => {
  let mockSupabase: ReturnType<typeof createMockSupabase>

  beforeEach(() => {
    mockSupabase = createMockSupabase()
  })

  describe('getBusinessProfile', () => {
    it('queries business_profiles table filtered by user_id', async () => {
      await getBusinessProfile(mockSupabase, 'user-123')

      expect(mockSupabase.from).toHaveBeenCalledWith('business_profiles')
      expect(mockSupabase._chain.select).toHaveBeenCalledWith('*')
      expect(mockSupabase._chain.eq).toHaveBeenCalledWith('user_id', 'user-123')
      expect(mockSupabase._chain.maybeSingle).toHaveBeenCalled()
    })

    it('returns data from the query', async () => {
      const mockProfile = { id: '1', company_name: 'Test Co' }
      mockSupabase._chain.maybeSingle.mockResolvedValue({ data: mockProfile, error: null })

      const result = await getBusinessProfile(mockSupabase, 'user-123')
      expect(result.data).toEqual(mockProfile)
    })
  })

  describe('getLatestAssessment', () => {
    it('queries assessments table filtered by user_id, ordered by created_at desc, limit 1', async () => {
      await getLatestAssessment(mockSupabase, 'user-123')

      expect(mockSupabase.from).toHaveBeenCalledWith('assessments')
      expect(mockSupabase._chain.select).toHaveBeenCalledWith('*')
      expect(mockSupabase._chain.eq).toHaveBeenCalledWith('user_id', 'user-123')
      expect(mockSupabase._chain.order).toHaveBeenCalledWith('created_at', { ascending: false })
      expect(mockSupabase._chain.limit).toHaveBeenCalledWith(1)
      expect(mockSupabase._chain.maybeSingle).toHaveBeenCalled()
    })

    it('returns data from the query', async () => {
      const mockAssessment = { id: '1', overall_score: 67 }
      mockSupabase._chain.maybeSingle.mockResolvedValue({ data: mockAssessment, error: null })

      const result = await getLatestAssessment(mockSupabase, 'user-123')
      expect(result.data).toEqual(mockAssessment)
    })
  })

  describe('getFindings', () => {
    it('queries findings table filtered by assessment_id', async () => {
      // For getFindings, the chain ends with select (returns array), not single
      mockSupabase._chain.order.mockResolvedValue({ data: [], error: null })

      await getFindings(mockSupabase, 'assessment-123')

      expect(mockSupabase.from).toHaveBeenCalledWith('findings')
      expect(mockSupabase._chain.select).toHaveBeenCalledWith('*')
      expect(mockSupabase._chain.eq).toHaveBeenCalledWith('assessment_id', 'assessment-123')
      expect(mockSupabase._chain.order).toHaveBeenCalledWith('severity', { ascending: true })
    })

    it('returns data from the query', async () => {
      const mockFindings = [
        { id: '1', title: 'Finding 1', severity: 'critical' },
        { id: '2', title: 'Finding 2', severity: 'high' },
      ]
      mockSupabase._chain.order.mockResolvedValue({ data: mockFindings, error: null })

      const result = await getFindings(mockSupabase, 'assessment-123')
      expect(result.data).toEqual(mockFindings)
    })
  })

  describe('getSubscription', () => {
    it('queries subscriptions table filtered by user_id', async () => {
      await getSubscription(mockSupabase, 'user-123')

      expect(mockSupabase.from).toHaveBeenCalledWith('subscriptions')
      expect(mockSupabase._chain.select).toHaveBeenCalledWith('*')
      expect(mockSupabase._chain.eq).toHaveBeenCalledWith('user_id', 'user-123')
      expect(mockSupabase._chain.maybeSingle).toHaveBeenCalled()
    })

    it('returns data from the query', async () => {
      const mockSub = { id: '1', plan: 'free', status: 'active' }
      mockSupabase._chain.maybeSingle.mockResolvedValue({ data: mockSub, error: null })

      const result = await getSubscription(mockSupabase, 'user-123')
      expect(result.data).toEqual(mockSub)
    })
  })
})
