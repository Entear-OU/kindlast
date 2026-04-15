import Link from 'next/link'

const freeFeatures = [
  'GDPR compliance score',
  'Top 3 findings with details',
  'Basic risk assessment',
  '1 re-assessment per month',
]

const premiumFeatures = [
  'Everything in Free',
  'Full findings list with recommendations',
  'AI Act risk classification',
  'PDF compliance report export',
  'Unlimited re-assessments',
  'Priority support',
]

export default function PricingPage() {
  return (
    <div className="mx-auto max-w-4xl px-4 py-16">
      <div className="mb-12 text-center">
        <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">
          Simple, Transparent Pricing
        </h1>
        <p className="mt-4 text-lg text-muted-foreground">
          Choose the plan that fits your compliance needs
        </p>
      </div>

      <div className="grid gap-8 md:grid-cols-2">
        {/* Free Tier */}
        <div className="flex flex-col rounded-xl border bg-card p-8">
          <h3 className="text-xl font-bold">Free</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Get started with basic compliance insights
          </p>
          <div className="mt-6">
            <span className="text-4xl font-bold">&euro;0</span>
            <span className="text-muted-foreground">/month</span>
          </div>
          <ul className="mt-8 flex-1 space-y-3">
            {freeFeatures.map((feature) => (
              <li key={feature} className="flex items-start gap-2 text-sm">
                <svg
                  className="mt-0.5 h-4 w-4 shrink-0 text-green-500"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  strokeWidth={2}
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M5 13l4 4L19 7"
                  />
                </svg>
                {feature}
              </li>
            ))}
          </ul>
          <Link
            href="/login"
            className="mt-8 inline-flex h-10 items-center justify-center rounded-lg border px-4 text-sm font-medium transition-colors hover:bg-muted"
          >
            Get Started Free
          </Link>
        </div>

        {/* Premium Tier */}
        <div className="relative flex flex-col rounded-xl border-2 border-primary bg-card p-8">
          <div className="absolute -top-3 right-4 rounded-full bg-primary px-3 py-0.5 text-xs font-medium text-primary-foreground">
            Recommended
          </div>
          <h3 className="text-xl font-bold">Premium</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Full compliance suite for growing businesses
          </p>
          <div className="mt-6">
            <span className="text-4xl font-bold">&euro;49</span>
            <span className="text-muted-foreground">/month</span>
          </div>
          <ul className="mt-8 flex-1 space-y-3">
            {premiumFeatures.map((feature) => (
              <li key={feature} className="flex items-start gap-2 text-sm">
                <svg
                  className="mt-0.5 h-4 w-4 shrink-0 text-green-500"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  strokeWidth={2}
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M5 13l4 4L19 7"
                  />
                </svg>
                {feature}
              </li>
            ))}
          </ul>
          <Link
            href="/login"
            className="mt-8 inline-flex h-10 items-center justify-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Start Premium Trial
          </Link>
        </div>
      </div>

      <div className="mt-12 text-center">
        <h2 className="text-xl font-bold">Feature Comparison</h2>
        <div className="mt-6 overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b">
                <th className="py-3 pr-4 font-medium">Feature</th>
                <th className="px-4 py-3 text-center font-medium">Free</th>
                <th className="px-4 py-3 text-center font-medium">Premium</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              <tr>
                <td className="py-3 pr-4">GDPR compliance score</td>
                <td className="px-4 py-3 text-center">Yes</td>
                <td className="px-4 py-3 text-center">Yes</td>
              </tr>
              <tr>
                <td className="py-3 pr-4">Top 3 findings</td>
                <td className="px-4 py-3 text-center">Yes</td>
                <td className="px-4 py-3 text-center">Yes</td>
              </tr>
              <tr>
                <td className="py-3 pr-4">Full findings list</td>
                <td className="px-4 py-3 text-center">-</td>
                <td className="px-4 py-3 text-center">Yes</td>
              </tr>
              <tr>
                <td className="py-3 pr-4">AI Act risk classification</td>
                <td className="px-4 py-3 text-center">-</td>
                <td className="px-4 py-3 text-center">Yes</td>
              </tr>
              <tr>
                <td className="py-3 pr-4">PDF compliance report</td>
                <td className="px-4 py-3 text-center">-</td>
                <td className="px-4 py-3 text-center">Yes</td>
              </tr>
              <tr>
                <td className="py-3 pr-4">Re-assessments</td>
                <td className="px-4 py-3 text-center">1/month</td>
                <td className="px-4 py-3 text-center">Unlimited</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
