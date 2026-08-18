import { redirect } from 'next/navigation'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { hasComplianceProfile } from '@/lib/onboarding/client'

/**
 * The surfaces a compliance profile makes meaningful (ENT-212, closing the
 * criterion inherited from ENT-166).
 *
 * # WHY THIS IS A ROUTE GROUP AND NOT A CHECK IN EACH PAGE
 *
 * ENT-166's surviving criterion is that a member of an organisation with no
 * profile is routed into onboarding rather than reaching a console whose data
 * is empty and whose writes are refused. A check repeated in every page is one
 * somebody eventually forgets to add to the next page, and the symptom is a
 * single route that strands people.
 *
 * It cannot live in `[org]/layout.tsx`, because onboarding is under that layout
 * too and a gate that redirected to itself would loop forever. A route group
 * changes no URL and gives the gate exactly the set of routes it should cover,
 * which is what route groups are for.
 *
 * # AND NOT EVERY AUTHENTICATED ROUTE, WHICH IS A DELIBERATE READING
 *
 * ENT-166 says "every authenticated route", and its own reasoning narrows that:
 * the problem is a console "whose data is empty and whose writes are refused".
 * Three surfaces are neither.
 *
 *   - Settings, including members, billing and the memory page. None of it is
 *     derived from the profile and none of its writes are refused without one.
 *     Bouncing an owner out of billing to answer eleven questions would be a
 *     worse product, and the memory page is where somebody goes to fix an
 *     answer afterwards.
 *   - Regulation. The corpus is the same law for every customer.
 *   - The audit log. It records what happened, which is a fact about the
 *     organisation rather than about its profile.
 *
 * Everything else is here: the dashboard, the feed, the record registers and
 * the agents. Those are the pages that promised a person something and then
 * refused it, and `/o/{slug}` is where anybody with no particular destination
 * lands, so nobody reaches the console without passing through this.
 *
 * # IT FAILS OPEN, ON PURPOSE
 *
 * `hasComplianceProfile` answers true when core-api cannot be reached. Bouncing
 * a signed-in person into onboarding during an outage would tell them their
 * organisation had been reset; letting them through leaves them on a page that
 * says the workspace is unavailable, which is true and recovers by itself.
 */
export default async function NeedsProfileLayout({
  children,
  params,
}: {
  children: React.ReactNode
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  // No redirect to sign-in here: the layout above already did that, and doing
  // it twice would mean two places deciding what an expired session means.
  if (!session) return children

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') return children

  const profiled = await hasComplianceProfile(
    session.accessToken,
    resolved.membership.orgId,
  )
  if (!profiled) redirect(orgPath(slug, '/onboarding'))

  return children
}
