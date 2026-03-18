import { describe, it, expect } from 'vitest'
import type {
  BusinessProfile,
  Assessment,
  Finding,
  Subscription,
} from '@/lib/types/database'

describe('lib/types/database', () => {
  it('BusinessProfile type works with valid data', () => {
    const profile: BusinessProfile = {
      id: '123e4567-e89b-12d3-a456-426614174000',
      user_id: '123e4567-e89b-12d3-a456-426614174001',
      company_name: 'Test Company',
      country: 'Estonia',
      industry: 'Technology',
      employee_count: 10,
      processes_personal_data: true,
      data_types: ['email', 'payment'],
      uses_ai_systems: false,
      ai_system_descriptions: null,
      third_party_processors: ['Stripe', 'Google Analytics'],
      transfers_data_outside_eu: false,
      has_dpo: false,
      has_privacy_policy: true,
      has_cookie_consent: true,
      has_breach_notification: false,
      has_dsr_process: false,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    }

    expect(profile.company_name).toBe('Test Company')
    expect(profile.data_types).toContain('email')
    expect(profile.country).toBe('Estonia')
  })

  it('Assessment type works with valid data', () => {
    const assessment: Assessment = {
      id: '123e4567-e89b-12d3-a456-426614174000',
      user_id: '123e4567-e89b-12d3-a456-426614174001',
      profile_id: '123e4567-e89b-12d3-a456-426614174002',
      type: 'gdpr',
      status: 'complete',
      overall_score: 67,
      risk_level: 'medium',
      result: { summary: 'test', findings: [] },
      created_at: '2024-01-01T00:00:00Z',
    }

    expect(assessment.type).toBe('gdpr')
    expect(assessment.status).toBe('complete')
    expect(assessment.overall_score).toBe(67)
  })

  it('Assessment type accepts all valid type values', () => {
    const gdpr: Assessment = {
      id: '1', user_id: '1', profile_id: '1',
      type: 'gdpr', status: 'pending',
      overall_score: null, risk_level: null, result: null,
      created_at: '2024-01-01T00:00:00Z',
    }
    const aiAct: Assessment = {
      id: '2', user_id: '2', profile_id: '2',
      type: 'ai_act', status: 'processing',
      overall_score: null, risk_level: null, result: null,
      created_at: '2024-01-01T00:00:00Z',
    }

    expect(gdpr.type).toBe('gdpr')
    expect(aiAct.type).toBe('ai_act')
  })

  it('Assessment type accepts all valid status values', () => {
    const statuses: Assessment['status'][] = ['pending', 'processing', 'complete', 'error']
    expect(statuses).toHaveLength(4)
  })

  it('Finding type works with valid data', () => {
    const finding: Finding = {
      id: '123e4567-e89b-12d3-a456-426614174000',
      assessment_id: '123e4567-e89b-12d3-a456-426614174001',
      user_id: '123e4567-e89b-12d3-a456-426614174002',
      category: 'lawful_basis',
      severity: 'critical',
      title: 'No lawful basis documented',
      description: 'You have not documented a lawful basis for processing.',
      recommendation: 'Document consent as your lawful basis.',
      gdpr_article: 'Art. 6',
      ai_act_article: null,
      is_resolved: false,
      resolved_at: null,
      created_at: '2024-01-01T00:00:00Z',
    }

    expect(finding.severity).toBe('critical')
    expect(finding.is_resolved).toBe(false)
    expect(finding.gdpr_article).toBe('Art. 6')
  })

  it('Finding type accepts all valid severity values', () => {
    const severities: Finding['severity'][] = ['critical', 'high', 'medium', 'low', 'pass']
    expect(severities).toHaveLength(5)
  })

  it('Subscription type works with valid data', () => {
    const sub: Subscription = {
      id: '123e4567-e89b-12d3-a456-426614174000',
      user_id: '123e4567-e89b-12d3-a456-426614174001',
      stripe_customer_id: 'cus_123',
      stripe_subscription_id: 'sub_123',
      plan: 'premium',
      status: 'active',
      current_period_end: '2024-12-31T00:00:00Z',
      created_at: '2024-01-01T00:00:00Z',
    }

    expect(sub.plan).toBe('premium')
    expect(sub.status).toBe('active')
  })

  it('Subscription type accepts all valid plan values', () => {
    const plans: Subscription['plan'][] = ['free', 'premium']
    expect(plans).toHaveLength(2)
  })

  it('Subscription type allows null for optional stripe fields', () => {
    const sub: Subscription = {
      id: '1',
      user_id: '1',
      stripe_customer_id: null,
      stripe_subscription_id: null,
      plan: 'free',
      status: 'active',
      current_period_end: null,
      created_at: '2024-01-01T00:00:00Z',
    }

    expect(sub.stripe_customer_id).toBeNull()
    expect(sub.stripe_subscription_id).toBeNull()
  })
})
