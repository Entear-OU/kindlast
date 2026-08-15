/**
 * The core-api client: attach the bearer, speak Connect, hand back plain data.
 *
 * `web` is a caller like any other. It holds no privileged position and gets
 * no side door: every request carries an access token that core-api verifies
 * for itself (§1.2). The one thing this module is careful about is which
 * requests carry a tenancy, and that turns out to be the whole subtlety.
 */
import type { Session } from './session'

/**
 * The header naming the organisation a request acts in. Mirrors
 * `interceptor.OrgHeader` in core-api, and the two have to agree exactly.
 */
export const ORG_HEADER = 'Kindlast-Org-Id'

export interface Membership {
  orgId: string
  orgName?: string
  /**
   * The URL segment this organisation's console routes hang off. Derived from
   * the name when the organisation was created and immutable afterwards, so it
   * is safe in a bookmark and does not follow a rename (ENT-198).
   */
  orgSlug?: string
  role?: string
}

export interface User {
  subject?: string
  email?: string
  name?: string
}

export interface CurrentUser {
  user?: User
  memberships: Membership[]
  activeOrgId?: string
  plan?: string
}

function baseUrl(): string | null {
  const base = process.env.KINDLAST_CORE_API_URL
  return base ? base.replace(/\/+$/, '') : null
}

interface CallOptions {
  accessToken: string
  /** Omitted entirely when null. See the note in `call`. */
  orgId?: string | null
  body?: unknown
}

/**
 * One Connect call.
 *
 * Returns null rather than throwing on any failure. A signed-in person whose
 * core-api call failed is still signed in, and turning that into an error page
 * would trade a degraded screen for a locked door.
 */
async function call<T>(
  method: string,
  options: CallOptions,
): Promise<T | null> {
  const base = baseUrl()
  if (!base) return null

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${options.accessToken}`,
  }

  // Omitted, never sent empty. The tenancy interceptor refuses a malformed
  // organisation id, and an empty string is malformed: sending one would turn
  // "I have no organisation yet" into "I sent you nonsense", which is a
  // refusal rather than a bootstrap.
  if (options.orgId) headers[ORG_HEADER] = options.orgId

  try {
    const response = await fetch(`${base}/${method}`, {
      method: 'POST',
      headers,
      body: JSON.stringify(options.body ?? {}),
      cache: 'no-store',
    })

    if (!response.ok) {
      console.error(`core-api ${method} -> ${response.status}`)
      return null
    }

    return (await response.json()) as T
  } catch (error) {
    console.error(`core-api ${method}`, error)
    return null
  }
}

/**
 * The bootstrap call.
 *
 * This is where just-in-time provisioning happens, so for a brand-new person
 * it is the request that creates their organisation and their membership.
 * Sent deliberately without an organisation header, because on a first sign-in
 * there is no organisation to name yet. That is also why SessionService
 * declares `openid` rather than a real permission: a caller who holds nothing
 * has to be able to reach the call that grants them something.
 */
export async function getCurrentUser(
  accessToken: string,
): Promise<CurrentUser | null> {
  const me = await call<CurrentUser>(
    'kindlast.core.v1.SessionService/GetCurrentUser',
    {
      accessToken,
    },
  )

  // Connect omits empty repeated fields rather than sending [], so a person
  // with no memberships comes back as {}. Normalising the one field that has
  // to be an array keeps every caller from having to know that.
  //
  // Spread rather than rebuilt from named fields, deliberately. An earlier
  // version returned `{ memberships }` alone, which silently dropped the user,
  // the active organisation and the plan; nothing failed, because every one of
  // those is optional, so the workspace simply stopped greeting anyone by name
  // and nobody noticed until slugs needed the same journey.
  if (!me) return null
  return { ...me, memberships: me.memberships ?? [] }
}

/** What redeeming an invitation tells the client about where to go next. */
export interface AcceptedInvitation {
  orgId?: string
  orgName?: string
  orgSlug?: string
  role?: string
}

/**
 * Redeems an invitation.
 *
 * Returns the joined organisation rather than a boolean, because the caller's
 * next move is a redirect and the URL is built from `orgSlug`. ENT-198 put the
 * slug on this response for exactly that reason: without it, a client that has
 * just joined an organisation has to make a second call to discover where it
 * lives.
 *
 * Null on any failure, including a token that was expired, already redeemed or
 * never real. core-api answers all three alike on purpose, so this cannot be
 * used to discover which tokens exist.
 */
export async function acceptInvitation(
  accessToken: string,
  token: string,
): Promise<AcceptedInvitation | null> {
  return call<AcceptedInvitation>(
    'kindlast.core.v1.OrgService/AcceptInvitation',
    {
      accessToken,
      body: { token },
    },
  )
}

/**
 * Which organisation a freshly signed-in person acts in.
 *
 * The first membership, and first is the server's ordering rather than an
 * arbitrary pick: someone who arrived through an invitation and also holds a
 * personal organisation has to land somewhere predictable. Null when there is
 * nothing to choose, and that nullness is load-bearing, since the alternative
 * is inventing a tenancy the caller does not have.
 */
export function activeOrgFrom(me: CurrentUser | null): string | null {
  return me?.memberships?.[0]?.orgId ?? null
}

/** The headers an authenticated request from an established session carries. */
export function sessionHeaders(session: Session): Record<string, string> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${session.accessToken}`,
  }
  if (session.orgId) headers[ORG_HEADER] = session.orgId
  return headers
}
