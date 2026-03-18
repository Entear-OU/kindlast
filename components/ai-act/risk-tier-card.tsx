import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card'
import { RiskTierBadge } from './risk-tier-badge'

interface RiskTierCardProps {
  name: string
  riskTier: 'unacceptable' | 'high' | 'limited' | 'minimal'
  reasoning: string
  obligations: string[]
  aiActArticles: string[]
  deadline: string
}

export function RiskTierCard({
  name,
  riskTier,
  reasoning,
  obligations,
  aiActArticles,
  deadline,
}: RiskTierCardProps) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-base">{name}</CardTitle>
          <RiskTierBadge tier={riskTier} />
        </div>
        <CardDescription>{reasoning}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <h4 className="mb-1 text-sm font-medium">Obligations</h4>
          <ul className="list-inside list-disc space-y-0.5 text-sm text-muted-foreground">
            {obligations.map((obligation, i) => (
              <li key={i}>{obligation}</li>
            ))}
          </ul>
        </div>
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">
            Articles: {aiActArticles.join(', ')}
          </span>
          <span className="text-muted-foreground">Deadline: {deadline}</span>
        </div>
      </CardContent>
    </Card>
  )
}
