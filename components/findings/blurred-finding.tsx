import Link from 'next/link'
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from '@/components/ui/card'
import type { Finding } from '@/lib/types/database'

interface BlurredFindingProps {
  finding: Finding
}

export function BlurredFinding({ finding }: BlurredFindingProps) {
  return (
    <Card className="relative overflow-hidden">
      <CardHeader>
        <CardTitle className="text-sm">{finding.title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="relative">
          <div className="select-none blur-sm">
            <p className="text-sm text-muted-foreground">
              {finding.description}
            </p>
            <p className="mt-2 text-sm">
              {finding.recommendation}
            </p>
          </div>
          <div className="absolute inset-0 flex flex-col items-center justify-center bg-background/60 backdrop-blur-[2px]">
            <p className="mb-2 text-sm font-medium">
              Upgrade to see full analysis
            </p>
            <Link
              href="/pricing"
              className="inline-flex h-8 items-center rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              Upgrade to Premium
            </Link>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
