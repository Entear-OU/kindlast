import { Card, CardContent, CardHeader } from '@/components/ui/card'
import type { Finding } from '@/lib/types/database'

interface FindingCardProps {
  finding: Finding
  children?: React.ReactNode
}

const severityStyles: Record<string, string> = {
  critical: 'text-red-700 bg-red-100',
  high: 'text-orange-700 bg-orange-100',
  medium: 'text-yellow-700 bg-yellow-100',
  low: 'text-blue-700 bg-blue-100',
  pass: 'text-green-700 bg-green-100',
}

const categoryLabels: Record<string, string> = {
  lawful_basis: 'Lawful Basis',
  consent: 'Consent',
  data_subject_rights: 'Data Subject Rights',
  privacy_policy: 'Privacy Policy',
  data_security: 'Data Security',
  breach_notification: 'Breach Notification',
  data_processing_records: 'Data Processing Records',
  dpo_requirement: 'DPO Requirement',
  cross_border_transfers: 'Cross-Border Transfers',
  cookie_compliance: 'Cookie Compliance',
  children_data: 'Children Data',
  data_minimization: 'Data Minimization',
}

export function FindingCard({ finding, children }: FindingCardProps) {
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium uppercase ${severityStyles[finding.severity]}`}
          >
            {finding.severity}
          </span>
          {finding.gdpr_article && (
            <span className="text-xs font-medium text-muted-foreground">
              {finding.gdpr_article}
            </span>
          )}
          <span className="text-xs text-muted-foreground">
            {categoryLabels[finding.category] || finding.category}
          </span>
          {finding.is_resolved && (
            <span className="inline-flex rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700">
              Resolved
            </span>
          )}
        </div>
        <h3 className="text-sm font-semibold">{finding.title}</h3>
      </CardHeader>
      <CardContent>
        <p className="mb-3 text-sm text-muted-foreground">{finding.description}</p>
        <div className="rounded-lg bg-muted/50 p-3">
          <p className="text-xs font-medium text-foreground">Recommendation</p>
          <p className="mt-1 text-sm text-muted-foreground">{finding.recommendation}</p>
        </div>
        {children}
      </CardContent>
    </Card>
  )
}
