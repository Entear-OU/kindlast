import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { Finding } from '@/lib/types/database'

interface FindingsSummaryProps {
  findings: Finding[]
}

const severities = ['critical', 'high', 'medium', 'low', 'pass'] as const

const severityStyles: Record<string, string> = {
  critical: 'text-red-600 bg-red-50',
  high: 'text-orange-600 bg-orange-50',
  medium: 'text-yellow-600 bg-yellow-50',
  low: 'text-blue-600 bg-blue-50',
  pass: 'text-green-600 bg-green-50',
}

export function FindingsSummary({ findings }: FindingsSummaryProps) {
  const counts = severities.reduce(
    (acc, severity) => {
      acc[severity] = findings.filter((f) => f.severity === severity).length
      return acc
    },
    {} as Record<string, number>
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle>Findings Summary</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-5 gap-2">
          {severities.map((severity) => (
            <div
              key={severity}
              className={`flex flex-col items-center gap-1 rounded-lg p-3 ${severityStyles[severity]}`}
            >
              <span
                data-testid={`count-${severity}`}
                className="text-2xl font-bold"
              >
                {counts[severity]}
              </span>
              <span className="text-xs font-medium capitalize">{severity}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
