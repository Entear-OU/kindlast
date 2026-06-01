'use client'

import { useState, useTransition } from 'react'

import { startCheckout } from '@/app/(authed)/billing/actions'
import type { Plan } from '@/lib/billing/plan'

/**
 * The pricing surface (ENT-85): Free vs Pro, with the upgrade CTA that starts
 * checkout. On success the server returns the hosted-checkout URL and we redirect
 * the whole window there; on Free→Pro the plan only flips once the webhook lands
 * (ENT-86), so there's nothing optimistic to show here.
 */

const PRO_PRICE = '€49'

const FREE_FEATURES = [
  'Onboarding + posture assessment',
  'Up to 3 findings',
  'ROPA capped at 3 activities',
  'Email notifications',
]

const PRO_FEATURES = [
  'Unlimited findings + full feed history',
  'Full ROPA, DSAR & AI Systems Register',
  'One-tap Executor actions',
  'Weekly briefing + monthly posture report',
]

export function BillingPlans({ plan, returnTo }: { plan: Plan; returnTo?: string }) {
  const [pending, startTransition] = useTransition()
  const [error, setError] = useState<string | null>(null)
  const isPro = plan === 'pro'

  function upgrade() {
    setError(null)
    startTransition(async () => {
      const res = await startCheckout(returnTo)
      if (res.ok) {
        window.location.href = res.url
      } else {
        setError(res.error)
      }
    })
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          {isPro ? 'You’re on Pro' : 'Upgrade to Pro'}
        </h1>
        <p className="text-sm text-muted-foreground">
          {isPro
            ? 'You have full access — unlimited findings, the complete registers, and one-tap actions.'
            : 'Let your agents act for you. Unlock one-tap Executor actions and your full compliance history.'}
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <PlanCard
          name="Free"
          price="€0"
          features={FREE_FEATURES}
          current={!isPro}
        />
        <PlanCard
          name="Pro"
          price={`${PRO_PRICE}/mo`}
          features={PRO_FEATURES}
          highlighted
          current={isPro}
          action={
            isPro ? null : (
              <button
                type="button"
                onClick={upgrade}
                disabled={pending}
                className="w-full rounded-lg bg-[#00C9A7] px-3 py-2 text-sm font-semibold text-zinc-950 transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {pending ? 'Starting checkout…' : `Upgrade — ${PRO_PRICE}/month`}
              </button>
            )
          }
        />
      </div>

      {error && <p className="text-sm text-rose-400">{error}</p>}
    </div>
  )
}

function PlanCard({
  name,
  price,
  features,
  highlighted = false,
  current = false,
  action = null,
}: {
  name: string
  price: string
  features: string[]
  highlighted?: boolean
  current?: boolean
  action?: React.ReactNode
}) {
  return (
    <div
      className={`flex flex-col gap-4 rounded-xl border p-5 ${
        highlighted ? 'border-[#00C9A7]/40 bg-[#00C9A7]/[0.05]' : 'border-border/60 bg-background'
      }`}
    >
      <div className="flex items-baseline justify-between">
        <h2 className="text-sm font-semibold text-foreground">{name}</h2>
        <span className="text-sm font-medium text-muted-foreground">{price}</span>
      </div>
      <ul className="flex flex-col gap-2 text-sm text-muted-foreground">
        {features.map((f) => (
          <li key={f} className="flex gap-2">
            <span aria-hidden="true" className="text-[#00C9A7]">
              ✓
            </span>
            {f}
          </li>
        ))}
      </ul>
      <div className="mt-auto">
        {current ? (
          <p className="rounded-lg border border-border/60 px-3 py-2 text-center text-sm text-muted-foreground">
            Current plan
          </p>
        ) : (
          action
        )}
      </div>
    </div>
  )
}
