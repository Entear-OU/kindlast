import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { Finding } from '@/lib/types/database'

interface RecentFindingsProps {
  findings: Finding[]
}

const severityStyles: Record<string, string> = {
  critical: 'text-red-700 bg-red-100',
  high: 'text-orange-700 bg-orange-100',
  medium: 'text-yellow-700 bg-yellow-100',
  low: 'text-blue-700 bg-blue-100',
  pass: 'text-green-700 bg-green-100',
}

export function RecentFindings({ findings }: RecentFindingsProps) {
  const topFindings = findings.slice(0, 5)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent Findings</CardTitle>
      </CardHeader>
      <CardContent>
        {topFindings.length === 0 ? (
          <p className="text-sm text-muted-foreground">No findings yet.</p>
        ) : (
          <div className="flex flex-col gap-3">
            {topFindings.map((finding) => (
              <div
                key={finding.id}
                className="flex flex-col gap-1 rounded-lg border p-3"
              >
                <div className="flex items-center gap-2">
                  <span
                    className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium capitalize ${severityStyles[finding.severity]}`}
                  >
                    {finding.severity}
                  </span>
                  {finding.gdpr_article && (
                    <span className="text-xs text-muted-foreground">
                      {finding.gdpr_article}
                    </span>
                  )}
                </div>
                <span className="text-sm font-medium">{finding.title}</span>
                <p className="text-xs text-muted-foreground line-clamp-2">
                  {finding.description}
                </p>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
