import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getCurrentUser, activeOrgFrom, ORG_HEADER } from '@/lib/auth/client'

/**
 * The core-api client, and specifically the call that bootstraps a person.
 *
 * GetCurrentUser is not an ordinary read. It is where just-in-time
 * provisioning happens, so the first time anyone signs in it is the call that
 * creates their organisation and their membership. Everything downstream, the
 * active-organisation header included, has nothing to name until it returns.
 *
 * Two properties are worth guarding here rather than only end to end. The
 * bearer has to be attached, because without it the call is anonymous and
 * provisioning has no subject to provision for. And the organisation header
 * must NOT be sent on this call: a caller on their first request has no
 * organisation yet, and sending an empty or invented one is refused by the
 * tenancy interceptor, which would make the bootstrap unreachable for exactly
 * the people who need it.
 */

const ACCESS_TOKEN = 'header.payload.signature'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('getCurrentUser', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    vi.stubEnv('KINDLAST_CORE_API_URL', 'http://core-api:8080')
    fetchMock.mockReset()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it('attaches the bearer and calls the Connect route for SessionService', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ memberships: [] }))

    await getCurrentUser(ACCESS_TOKEN)

    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, init] = fetchMock.mock.calls[0]

    expect(url).toBe(
      'http://core-api:8080/kindlast.core.v1.SessionService/GetCurrentUser',
    )
    expect(init.method).toBe('POST')
    expect(init.headers.Authorization).toBe(`Bearer ${ACCESS_TOKEN}`)
  })

  it('sends no organisation header, because the caller may not have one yet', async () => {
    // The bootstrap call is the one request in the system that is legitimately
    // made without a tenancy. Sending the header empty is not the same as
    // omitting it: the interceptor refuses a malformed value.
    fetchMock.mockResolvedValue(jsonResponse({ memberships: [] }))

    await getCurrentUser(ACCESS_TOKEN)

    const [, init] = fetchMock.mock.calls[0]
    expect(Object.keys(init.headers)).not.toContain(ORG_HEADER)
  })

  it('returns the memberships provisioning created', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        memberships: [
          { orgId: 'a0000000-0000-4000-8000-000000000001', role: 'owner' },
        ],
      }),
    )

    const me = await getCurrentUser(ACCESS_TOKEN)

    expect(me?.memberships).toHaveLength(1)
    expect(me?.memberships[0].orgId).toBe(
      'a0000000-0000-4000-8000-000000000001',
    )
  })

  it('keeps the whole answer, not only the memberships', async () => {
    // The response carries the signed-in person, the active organisation and
    // the plan, and every one of them has a caller: the workspace greets
    // someone by email, and ENT-198 routes on the slug. Normalising the
    // memberships is not a licence to drop the rest, and dropping it is
    // invisible at the call site because the fields are all optional.
    fetchMock.mockResolvedValue(
      jsonResponse({
        user: {
          email: 'ada@example.com',
          name: 'Ada Lovelace',
          emailVerified: true,
        },
        memberships: [
          {
            orgId: 'a0000000-0000-4000-8000-000000000001',
            orgName: 'Ada Lovelace',
            orgSlug: 'ada-lovelace',
            role: 'owner',
          },
        ],
        activeOrgId: 'a0000000-0000-4000-8000-000000000001',
        plan: 'free',
      }),
    )

    const me = await getCurrentUser(ACCESS_TOKEN)

    expect(me?.user?.email).toBe('ada@example.com')
    expect(me?.activeOrgId).toBe('a0000000-0000-4000-8000-000000000001')
    expect(me?.plan).toBe('free')
    expect(me?.memberships[0].orgSlug).toBe('ada-lovelace')
  })

  it('returns null rather than throwing when core-api refuses', async () => {
    // A failed bootstrap must not strand someone who holds a valid session on
    // an error page. They are signed in; the call can be retried on the next
    // navigation.
    fetchMock.mockResolvedValue(
      jsonResponse({ code: 'permission_denied' }, 403),
    )

    expect(await getCurrentUser(ACCESS_TOKEN)).toBeNull()
  })

  it('returns null when core-api is not configured, rather than calling undefined', async () => {
    vi.stubEnv('KINDLAST_CORE_API_URL', '')

    expect(await getCurrentUser(ACCESS_TOKEN)).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('activeOrgFrom', () => {
  it('picks the single membership a newly provisioned person has', () => {
    expect(
      activeOrgFrom({ memberships: [{ orgId: 'org-1', role: 'owner' }] }),
    ).toBe('org-1')
  })

  it('is null when there are no memberships, so nothing invents a tenancy', () => {
    // Nullness has to survive to the session. An empty string in the header
    // position is precisely the malformed value the interceptor refuses.
    expect(activeOrgFrom({ memberships: [] })).toBeNull()
    expect(activeOrgFrom(null)).toBeNull()
  })

  it('prefers the first membership when someone belongs to several', () => {
    // Deterministic rather than arbitrary: an invited user who also has a
    // personal organisation must land somewhere predictable, and the ordering
    // core-api returns is the one the server chose.
    expect(
      activeOrgFrom({
        memberships: [
          { orgId: 'invited-org', role: 'member' },
          { orgId: 'personal-org', role: 'owner' },
        ],
      }),
    ).toBe('invited-org')
  })
})
