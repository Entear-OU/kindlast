import Link from 'next/link'
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from '@/components/ui/card'

export function UpgradePrompt() {
  return (
    <Card className="border-primary/20 bg-primary/5">
      <CardHeader>
        <CardTitle>Upgrade to Premium</CardTitle>
        <CardDescription>
          Unlock the full power of Kindlast compliance analysis
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-2 text-sm">
          <p className="text-2xl font-bold">
            &euro;49<span className="text-sm font-normal text-muted-foreground">/month</span>
          </p>
          <ul className="space-y-1 text-muted-foreground">
            <li>Full findings list with detailed recommendations</li>
            <li>AI Act risk classification</li>
            <li>PDF compliance report export</li>
            <li>Unlimited re-assessments</li>
          </ul>
        </div>
      </CardContent>
      <CardFooter>
        <Link
          href="/pricing"
          className="inline-flex h-9 w-full items-center justify-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          View Pricing
        </Link>
      </CardFooter>
    </Card>
  )
}
