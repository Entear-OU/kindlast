import { describe, it, expect } from 'vitest'
import { FindingSchema, AssessmentResultSchema } from '@/lib/ai/schemas'

describe('FindingSchema', () => {
  const validFinding = {
    category: 'lawful_basis',
    severity: 'critical',
    title: 'No lawful basis documented',
    description: 'The business has not documented a lawful basis for processing personal data.',
    recommendation: 'Document the lawful basis under GDPR Art. 6 for each processing activity.',
    gdpr_article: 'Art. 6',
  }

  it('accepts valid finding data', () => {
    const result = FindingSchema.safeParse(validFinding)
    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.category).toBe('lawful_basis')
      expect(result.data.severity).toBe('critical')
      expect(result.data.title).toBe('No lawful basis documented')
    }
  })

  it('accepts all valid categories', () => {
    const categories = [
      'lawful_basis', 'consent', 'data_subject_rights', 'privacy_policy',
      'data_security', 'breach_notification', 'data_processing_records',
      'dpo_requirement', 'cross_border_transfers', 'cookie_compliance',
      'children_data', 'data_minimization',
    ]
    for (const category of categories) {
      const result = FindingSchema.safeParse({ ...validFinding, category })
      expect(result.success).toBe(true)
    }
  })

  it('accepts all valid severities', () => {
    const severities = ['critical', 'high', 'medium', 'low', 'pass']
    for (const severity of severities) {
      const result = FindingSchema.safeParse({ ...validFinding, severity })
      expect(result.success).toBe(true)
    }
  })

  it('rejects invalid severity', () => {
    const result = FindingSchema.safeParse({ ...validFinding, severity: 'extreme' })
    expect(result.success).toBe(false)
  })

  it('rejects invalid category', () => {
    const result = FindingSchema.safeParse({ ...validFinding, category: 'invalid_category' })
    expect(result.success).toBe(false)
  })

  it('requires all fields', () => {
    const result = FindingSchema.safeParse({})
    expect(result.success).toBe(false)
  })
})

describe('AssessmentResultSchema', () => {
  const validResult = {
    overall_score: 67,
    risk_level: 'medium',
    summary: 'The business has several compliance gaps that need attention.',
    findings: [
      {
        category: 'lawful_basis',
        severity: 'critical',
        title: 'No lawful basis documented',
        description: 'No documented lawful basis.',
        recommendation: 'Document lawful basis.',
        gdpr_article: 'Art. 6',
      },
    ],
  }

  it('accepts valid assessment result', () => {
    const result = AssessmentResultSchema.safeParse(validResult)
    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.overall_score).toBe(67)
      expect(result.data.risk_level).toBe('medium')
      expect(result.data.findings).toHaveLength(1)
    }
  })

  it('rejects score below 0', () => {
    const result = AssessmentResultSchema.safeParse({ ...validResult, overall_score: -1 })
    expect(result.success).toBe(false)
  })

  it('rejects score above 100', () => {
    const result = AssessmentResultSchema.safeParse({ ...validResult, overall_score: 101 })
    expect(result.success).toBe(false)
  })

  it('accepts score of 0', () => {
    const result = AssessmentResultSchema.safeParse({ ...validResult, overall_score: 0 })
    expect(result.success).toBe(true)
  })

  it('accepts score of 100', () => {
    const result = AssessmentResultSchema.safeParse({ ...validResult, overall_score: 100 })
    expect(result.success).toBe(true)
  })

  it('rejects invalid risk_level', () => {
    const result = AssessmentResultSchema.safeParse({ ...validResult, risk_level: 'extreme' })
    expect(result.success).toBe(false)
  })

  it('requires all fields', () => {
    const result = AssessmentResultSchema.safeParse({})
    expect(result.success).toBe(false)
  })

  it('requires summary field', () => {
    const { summary, ...withoutSummary } = validResult
    const result = AssessmentResultSchema.safeParse(withoutSummary)
    expect(result.success).toBe(false)
  })

  it('requires findings array', () => {
    const { findings, ...withoutFindings } = validResult
    const result = AssessmentResultSchema.safeParse(withoutFindings)
    expect(result.success).toBe(false)
  })

  it('accepts empty findings array', () => {
    const result = AssessmentResultSchema.safeParse({ ...validResult, findings: [] })
    expect(result.success).toBe(true)
  })
})
