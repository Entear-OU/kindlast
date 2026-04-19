import { queryRAGWithSchema, type LegacyCitation } from '@/lib/api/gateway'
import { AssessmentResultSchema, type AssessmentResult } from './schemas'
import type { BusinessProfile } from '@/lib/types/database'

// Re-export Citation type for consumers
export type Citation = LegacyCitation

export interface AssessmentResultWithCitations extends AssessmentResult {
  citations: LegacyCitation[]
}

const GDPR_SYSTEM_PROMPT = `You are an expert EU data protection consultant specializing in GDPR compliance for small and medium-sized enterprises. You assess businesses against the full scope of the General Data Protection Regulation (EU) 2016/679.

Your assessment must be:
- Specific to the business context provided (industry, size, data types, tools)
- Actionable - every finding must include a concrete next step the business can take
- Accurate - cite the specific GDPR article for each finding
- Proportionate - consider the business size when assessing risk severity

Score rubric:
- 90-100: Largely compliant, minor improvements needed
- 70-89: Mostly compliant, some significant gaps
- 50-69: Partially compliant, multiple areas need attention
- 30-49: Significant non-compliance risks
- 0-29: Critical non-compliance, immediate action required

Use the provided regulatory context to ground your assessment in primary sources. Every finding should reference specific GDPR articles.`

function buildAssessmentPrompt(profile: BusinessProfile): string {
  return `Assess the GDPR compliance of the following business:

Company: ${profile.company_name}
Country: ${profile.country}
Industry: ${profile.industry}
Employees: ${profile.employee_count}

Data Processing:
- Collects personal data: ${profile.processes_personal_data}
- Data types collected: ${profile.data_types?.join(', ')}
- Third-party processors: ${profile.third_party_processors?.join(', ')}
- Transfers data outside EU: ${profile.transfers_data_outside_eu}

Current Compliance Measures:
- Has privacy policy: ${profile.has_privacy_policy}
- Has cookie consent: ${profile.has_cookie_consent}
- Has DPO: ${profile.has_dpo}
- Has breach notification process: ${profile.has_breach_notification}
- Has data subject request process: ${profile.has_dsr_process}

AI Systems: ${profile.uses_ai_systems ? JSON.stringify(profile.ai_system_descriptions) : 'None'}

Provide a comprehensive GDPR compliance assessment with specific findings and actionable recommendations.`
}

/**
 * Assesses GDPR compliance for a business profile using the backend RAG service.
 *
 * @param profile - The business profile to assess
 * @returns Assessment result with findings and citations from regulatory sources
 */
export async function assessGDPRCompliance(
  profile: BusinessProfile
): Promise<AssessmentResultWithCitations> {
  const response = await queryRAGWithSchema({
    query: buildAssessmentPrompt(profile),
    systemPrompt: GDPR_SYSTEM_PROMPT,
    schema: AssessmentResultSchema,
    collection: 'gdpr',
    topK: 10,
    temperature: 0.3,
  })

  return {
    ...response.data,
    citations: response.citations,
  }
}

/**
 * Assesses GDPR compliance without citations (for backward compatibility).
 *
 * @param profile - The business profile to assess
 * @returns Assessment result with findings
 */
export async function assessGDPRComplianceSimple(
  profile: BusinessProfile
): Promise<AssessmentResult> {
  const result = await assessGDPRCompliance(profile)
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { citations, ...assessmentResult } = result
  return assessmentResult
}
