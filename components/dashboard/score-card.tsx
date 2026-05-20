import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

interface ScoreCardProps {
  score: number
  riskLevel: string
}

function getScoreColor(score: number): string {
  if (score >= 90) return 'compliant'
  if (score >= 70) return 'mostly-compliant'
  if (score >= 50) return 'medium'
  if (score >= 30) return 'high'
  return 'critical'
}

function getScoreLabel(score: number): string {
  if (score >= 90) return 'Compliant'
  if (score >= 70) return 'Mostly Compliant'
  if (score >= 50) return 'Medium Risk'
  if (score >= 30) return 'High Risk'
  return 'Critical Risk'
}

const colorClasses: Record<string, string> = {
  critical: 'text-red-600 bg-red-50 border-red-200',
  high: 'text-orange-600 bg-orange-50 border-orange-200',
  medium: 'text-yellow-600 bg-yellow-50 border-yellow-200',
  'mostly-compliant': 'text-blue-600 bg-blue-50 border-blue-200',
  compliant: 'text-green-600 bg-green-50 border-green-200',
}

export function ScoreCard({ score, riskLevel }: ScoreCardProps) {
  const scoreColor = getScoreColor(score)
  const label = getScoreLabel(score)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Your GDPR Compliance Score</CardTitle>
      </CardHeader>
      <CardContent>
        <div
          data-score-color={scoreColor}
          className={`flex flex-col items-center gap-3 rounded-lg border p-6 ${colorClasses[scoreColor] || ''}`}
        >
          <span className="text-5xl font-bold">{score}</span>
          <span className="text-sm font-medium uppercase">{label}</span>
          <div className="mt-2 h-2 w-full rounded-full bg-gray-200">
            <div
              className="h-2 rounded-full bg-current"
              style={{ width: `${score}%` }}
            />
          </div>
          <span className="text-xs">{score}/100</span>
        </div>
      </CardContent>
    </Card>
  )
}
