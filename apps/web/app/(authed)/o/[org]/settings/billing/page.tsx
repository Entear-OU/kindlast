import { notFound, redirect } from 'next/navigation'

import { BillingState } from '@/components/settings/billing-state'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { getBilling } from '@/lib/billing/client'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * Billing (ENT-210).
 *
 * # THREE STATES, AND THE MIDDLE ONE IS THE POINT
 *
 * A deployment with no payment provider shows no upgrade path at all, because a
 * self-hoster has no Stripe account and must not need one (§18.1). A deployment
 * with one shows the plan and, on free, the fact that upgrading exists.
 *
 * The state worth designing for is neither: a `pro` subscription whose payment
 * has failed. Entitlement has already dropped to free, and saying only "you are
 * on the free plan" would tell a paying customer they downgraded themselves.
 * `status` is reported separately from `plan` for exactly this, and the page
 * says which of the two is happening.
 *
 * # WHY THERE IS NO CHECKOUT BUTTON YET
 *
 * Deliberate, and not an omission. Starting a checkout means a session that
 * redirects to the provider and returns, which is its own piece of work; what
 * exists today is the half that matters for correctness, which is that the
 * webhook can move a plan and the console reports it honestly. A button that
 * led nowhere would be the same mistake ENT-202 made with the invite control
 * and ENT-219 had to undo.
 */
export default async function BillingPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/settings/billing'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Billing" />

  const { membership } = resolved

  // Owner-only, and core-api refuses a member regardless. Rendering 404 rather
  // than a refusal message: a member has no business knowing this page exists
  // for their organisation, and "you are not allowed" is itself information.
  if (membership.role !== 'owner') notFound()

  const result = await getBilling(session.accessToken, membership.orgId)

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Billing
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        {membership.orgName || slug}
      </p>

      <section className="mt-10">
        {!result.ok ? (
          // Null is not "no subscription". Rendering a plan here would tell an
          // owner something about their billing that this page did not manage
          // to read.
          <p className="text-sm text-muted-foreground">
            Could not load billing for this organisation. Reload to try again.
          </p>
        ) : (
          <BillingState billing={result.value} />
        )}
      </section>
    </main>
  )
}
