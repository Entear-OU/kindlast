import { describe, it, expect } from 'vitest'
import { resolveOrgFrom, landingPathFor, orgPath } from '@/lib/auth/org'

/**
 * Resolving the organisation named in the URL (ENT-198).
 *
 * The rule is small and the acceptance criteria around it are unusually
 * specific, because getting it wrong is a tenancy bug rather than a routing
 * inconvenience:
 *
 *   - a slug the caller is not a member of must be 404, never 403, and never
 *     a quiet redirect into an organisation they DO belong to. A 403 confirms
 *     the organisation exists, and a redirect changes what a URL means
 *     underneath someone who bookmarked it
 *   - membership is decided from the caller's own memberships and nothing
 *     else, so "no such organisation" and "not yours" are the same
 *     computation and cannot drift apart into an oracle
 *   - an unreachable core-api is NOT "not a member". Collapsing the two would
 *     turn an outage into a 404 on a page the person is entitled to see, and
 *     would read as their organisation having been deleted
 *
 * Tested against the pure rule rather than through a fetch mock, which is the
 * reason the rule is separated from the call that feeds it.
 */

const ACME = {
  orgId: 'a0000000-0000-4000-8000-000000000001',
  orgName: 'Acme Ltd',
  orgSlug: 'acme-ltd',
  role: 'owner',
}

const BEDROCK = {
  orgId: 'a0000000-0000-4000-8000-000000000002',
  orgName: 'Bedrock',
  orgSlug: 'bedrock',
  role: 'member',
}

describe('resolveOrgFrom', () => {
  it('resolves the membership whose slug matches the URL', () => {
    const resolved = resolveOrgFrom({ memberships: [ACME, BEDROCK] }, 'bedrock')

    expect(resolved.status).toBe('ok')
    if (resolved.status !== 'ok') return
    expect(resolved.membership.orgId).toBe(BEDROCK.orgId)
    expect(resolved.membership.role).toBe('member')
  })

  it('picks by slug and not by position, so two organisations do not blur', () => {
    // The criterion: a member of two organisations sees the correct data for
    // each by URL alone, with no cookie involved. Asking for the second must
    // not hand back the first just because it happens to be listed first.
    const me = { memberships: [ACME, BEDROCK] }

    const first = resolveOrgFrom(me, 'acme-ltd')
    const second = resolveOrgFrom(me, 'bedrock')

    expect(first.status === 'ok' && first.membership.orgId).toBe(ACME.orgId)
    expect(second.status === 'ok' && second.membership.orgId).toBe(BEDROCK.orgId)
  })

  it('reports a slug the caller does not belong to as not-a-member, for a 404', () => {
    expect(resolveOrgFrom({ memberships: [ACME] }, 'bedrock').status).toBe('not-a-member')
  })

  it('answers identically for an organisation that does not exist at all', () => {
    // Indistinguishable by construction: only the caller's own memberships are
    // ever consulted, so this cannot tell the two apart even in principle.
    // That is what stops the URL becoming an oracle for which organisations
    // exist.
    const me = { memberships: [ACME] }

    expect(resolveOrgFrom(me, 'bedrock').status).toBe(
      resolveOrgFrom(me, 'no-such-org-anywhere').status,
    )
  })

  it('never resolves a slug that differs only in case or spacing', () => {
    const me = { memberships: [ACME] }

    expect(resolveOrgFrom(me, 'ACME-LTD').status).toBe('not-a-member')
    expect(resolveOrgFrom(me, 'acme ltd').status).toBe('not-a-member')
  })

  it('distinguishes an unreachable core-api from a slug that is not the caller’s', () => {
    // The distinction that matters: an outage must not render as a 404 on a
    // page this person is entitled to see.
    const resolved = resolveOrgFrom(null, 'acme-ltd')

    expect(resolved.status).toBe('unavailable')
    expect(resolved.status).not.toBe('not-a-member')
  })

  it('treats a membership with no slug as unroutable rather than matching it', () => {
    // org_slug is optional on the wire, so without the guard every membership
    // from an older core-api would match a request for the empty slug.
    const me = { memberships: [{ orgId: ACME.orgId, role: 'owner' }] }

    expect(resolveOrgFrom(me, '').status).toBe('not-a-member')
  })
})

describe('orgPath', () => {
  it('builds the console path for an organisation', () => {
    expect(orgPath('acme-ltd')).toBe('/o/acme-ltd')
    expect(orgPath('acme-ltd', '/feed')).toBe('/o/acme-ltd/feed')
  })
})

describe('landingPathFor', () => {
  it('sends a freshly provisioned person into their organisation', () => {
    expect(landingPathFor({ memberships: [ACME] })).toBe('/o/acme-ltd')
  })

  it('prefers the first membership, matching the ordering core-api chose', () => {
    // An invited user who also holds a personal organisation has to land
    // somewhere predictable rather than somewhere arbitrary.
    expect(landingPathFor({ memberships: [BEDROCK, ACME] })).toBe('/o/bedrock')
  })

  it('is null when there is nowhere to land, so nothing invents a URL', () => {
    // Nullness is load-bearing: a caller that built `/o/undefined` would
    // produce a URL that 404s and reads as data loss rather than as the
    // absence of an organisation.
    expect(landingPathFor({ memberships: [] })).toBeNull()
    expect(landingPathFor(null)).toBeNull()
    expect(landingPathFor({ memberships: [{ orgId: ACME.orgId, role: 'owner' }] })).toBeNull()
  })
})
