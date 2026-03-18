import { z } from 'zod'

export const FindingSchema = z.object({
  category: z.enum([
    'lawful_basis',
    'consent',
    'data_subject_rights',
    'privacy_policy',
    'data_security',
    'breach_notification',
    'data_processing_records',
    'dpo_requirement',
    'cross_border_transfers',
    'cookie_compliance',
    'children_data',
    'data_minimization',
  ]),
  severity: z.enum(['critical', 'high', 'medium', 'low', 'pass']),
  title: z.string(),
  description: z.string(),
  recommendation: z.string(),
  gdpr_article: z.string(),
})

export const AssessmentResultSchema = z.object({
  overall_score: z.number().min(0).max(100),
  risk_level: z.enum(['low', 'medium', 'high', 'critical']),
  summary: z.string(),
  findings: z.array(FindingSchema),
})

export type FindingResult = z.infer<typeof FindingSchema>
export type AssessmentResult = z.infer<typeof AssessmentResultSchema>
