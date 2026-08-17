import type { Billing } from '@/lib/billing/client'

/**
 * What an organisation is on, and what this deployment can offer it (ENT-210).
 *
 * # THE STATE THIS EXISTS TO GET RIGHT
 *
 * `plan` is the entitlement in force, so a `pro` subscription whose payment has
 * failed already reads as `free`. Rendering that alone would tell a paying
 * customer they downgraded themselves. `status` is carried separately for
 * exactly this reason, and the difference between "you cancelled" and "your
 * card was declined" is the difference between a support ticket and a churned
 * customer.
 *
 * # THREE DEPLOYMENTS, NOT ONE
 *
 * A self-hoster has no payment provider and must never need one, so
 * `billingConfigured` false means no upgrade path is rendered at all rather
 * than a checkout that leads nowhere. An operator may also wire a provider
 * before turning gating on, which is why `gatingEnabled` is separate: nothing
 * is withheld, so nothing should be advertised as withheld.
 */
export function BillingState({ billing }: { billing: Billing }) {
  const plan = billing.plan ?? 'free'
  const paymentFailed = billing.status === 'past_due'
  const cancelled = billing.status === 'canceled'

  return (
    <div className="space-y-6">
      <div className="rounded-xl border border-border/60 bg-background p-5">
        <p className="text-xs uppercase tracking-[0.08em] text-muted-foreground">
          Current plan
        </p>
        <p className="mt-1 text-lg font-semibold text-foreground">{plan}</p>

        {billing.currentPeriodEnd ? (
          <p className="mt-1 text-sm text-muted-foreground">
            Current period ends{' '}
            <time dateTime={billing.currentPeriodEnd}>
              {new Date(billing.currentPeriodEnd).toLocaleDateString()}
            </time>
            .
          </p>
        ) : null}
      </div>

      {/* Entitlement has already dropped, and an owner told only "you are on
          free" would reasonably think they had cancelled. */}
      {paymentFailed ? (
        <p
          role="alert"
          className="rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm text-destructive"
        >
          A payment for this organisation has not gone through, so paid features
          are paused. Updating the payment method with the provider restores
          them; nothing in your compliance record has been removed.
        </p>
      ) : null}

      {cancelled ? (
        <p role="status" className="text-sm text-muted-foreground">
          This subscription is cancelled. Your compliance record stays readable
          and exportable: nothing is withheld because a plan lapsed.
        </p>
      ) : null}

      {!billing.billingConfigured ? (
        // A self-hosted deployment. No upgrade path, no checkout, no plan
        // comparison, and a sentence saying why rather than an empty panel.
        <p className="text-sm text-muted-foreground">
          This deployment is self-hosted and sells nothing, so there is no plan
          to change. Every feature is available.
        </p>
      ) : !billing.gatingEnabled ? (
        <p className="text-sm text-muted-foreground">
          Plan limits are not being applied on this deployment, so nothing is
          withheld on the free plan.
        </p>
      ) : plan === 'free' ? (
        <p className="text-sm text-muted-foreground">
          On the free plan, manually added Article 30 entries are capped.
          Records the Executor creates from an approved finding are never
          capped, because they are part of your compliance record.
        </p>
      ) : null}
    </div>
  )
}
