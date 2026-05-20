import { describe, it, expect, vi } from 'vitest'

// Mock @react-pdf/renderer
vi.mock('@react-pdf/renderer', () => ({
  Document: ({ children }: any) => children,
  Page: ({ children }: any) => children,
  View: ({ children }: any) => children,
  Text: ({ children }: any) => children,
  StyleSheet: {
    create: (styles: any) => styles,
  },
  Font: {
    register: vi.fn(),
  },
}))

import { ComplianceReport } from '@/lib/pdf/compliance-report'

describe('ComplianceReport', () => {
  it('creates a report component without errors', () => {
    const props = {
      companyName: 'Test Company',
      date: '2026-03-18',
      overallScore: 72,
      riskLevel: 'medium' as const,
      summary: 'Your company is partially compliant with GDPR.',
      findings: [
        {
          id: '1',
          category: 'privacy_policy',
          severity: 'high' as const,
          title: 'Missing Privacy Policy',
          description: 'No privacy policy found on website.',
          recommendation: 'Create and publish a GDPR-compliant privacy policy.',
          gdpr_article: 'Art. 13',
        },
        {
          id: '2',
          category: 'consent',
          severity: 'medium' as const,
          title: 'Cookie consent incomplete',
          description: 'Cookie banner does not allow granular consent.',
          recommendation: 'Implement a cookie consent mechanism with opt-in choices.',
          gdpr_article: 'Art. 7',
        },
      ],
    }

    // The component should be callable without throwing
    const result = ComplianceReport(props)
    expect(result).toBeDefined()
  })

  it('handles empty findings array', () => {
    const props = {
      companyName: 'Empty Co',
      date: '2026-03-18',
      overallScore: 95,
      riskLevel: 'low' as const,
      summary: 'Excellent compliance posture.',
      findings: [],
    }

    const result = ComplianceReport(props)
    expect(result).toBeDefined()
  })
})
