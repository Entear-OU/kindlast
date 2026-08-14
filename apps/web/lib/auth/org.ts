/**
 * Resolving the organisation named in the URL (ENT-198, §20.1, §22.4).
 *
 * The console is scoped by URL rather than by a cookie, and that is a
 * correctness decision rather than an aesthetic one. A consultant serving
 * three client companies has three tabs open; with the active organisation in
 * a cookie, switching in one tab silently changes what the other two are
 * showing, and an approval clicked in a stale tab is recorded against the
 * wrong company. In a compliance product that is the failure the whole design
 * exists to prevent. So the URL says which organisation, every time, and
 * nothing about tenancy is remembered between requests.
 *
 * Its own module rather than logic inside the layout, so the rule can be
 * tested directly. A layout is an async server component: exercising this
 * through one would mean rendering React to assert a tenancy decision.
 */
import { cache } from 'react'

import { getCurrentUser, type CurrentUser, type Membership } from './client'

/**
 * What resolving a slug can conclude.
 *
 * Three outcomes and not two, because `unavailable` must never collapse into
 * `not-a-member`. They lead to different responses: one is a 404, and doing
 * that during a core-api outage tells someone their organisation is gone.
 */
export type OrgResolution =
  | { status: 'ok'; me: CurrentUser; membership: Membership }
  | { status: 'not-a-member'; me: CurrentUser }
  | { status: 'unavailable' }

/**
 * One GetCurrentUser per request, however many components ask.
 *
 * The layout resolves the slug and the page inside it needs the same answer.
 * React's `cache` dedupes them for the lifetime of a single request, so this
 * costs one round trip rather than one per component, without either of them
 * having to pass the result to the other (a layout cannot hand props to a
 * page).
 */
export const loadCurrentUser = cache(getCurrentUser)

/**
 * Resolve a URL slug to the caller's membership in it.
 *
 * Decided entirely from the caller's own memberships, which is what makes
 * "there is no such organisation" and "that one is not yours" the same
 * computation. They must be indistinguishable: a 403, or any answer that
 * differs between the two, confirms that a given organisation exists to
 * someone who has no business knowing. Since only the caller's memberships are
 * ever consulted, this module could not tell them apart even if it wanted to.
 */
export function resolveOrgFrom(
  me: CurrentUser | null,
  slug: string,
): OrgResolution {
  // Null means the call failed, which is not the same as belonging to
  // nothing. See the note on OrgResolution.
  if (!me) return { status: 'unavailable' }

  // An exact match. No case folding and no trimming: the slug is already
  // normalised at the point it is minted, so anything that differs is a
  // different slug rather than a near miss to be helpful about.
  //
  // The `m.orgSlug &&` guard is not defensive noise. org_slug is optional on
  // the wire, so without it every membership from a core-api that does not
  // send one yet would match a request for the empty slug.
  const membership = me.memberships.find((m) => m.orgSlug && m.orgSlug === slug)
  if (!membership) return { status: 'not-a-member', me }

  return { status: 'ok', me, membership }
}

/**
 * The same rule, against the caller's live memberships.
 *
 * Deliberately one line: everything worth testing is in `resolveOrgFrom`, and
 * this is the I/O that would otherwise force a tenancy rule to be tested
 * through a fetch mock.
 */
export async function resolveOrg(
  accessToken: string,
  slug: string,
): Promise<OrgResolution> {
  return resolveOrgFrom(await loadCurrentUser(accessToken), slug)
}

/** The console path for an organisation. One place that knows the shape. */
export function orgPath(slug: string, rest = ''): string {
  return `/o/${slug}${rest}`
}

/**
 * Where a person with no particular destination should land.
 *
 * The first membership, and first is core-api's ordering rather than an
 * arbitrary pick: someone who arrived through an invitation and also holds a
 * personal organisation has to land somewhere predictable.
 *
 * Null when there is nowhere, and that nullness is load-bearing. A caller that
 * invented `/o/undefined` would produce a URL that 404s and looks like data
 * loss rather than like the absence of an organisation.
 */
export function landingPathFor(me: CurrentUser | null): string | null {
  const slug = me?.memberships?.find((m) => m.orgSlug)?.orgSlug
  return slug ? orgPath(slug) : null
}
