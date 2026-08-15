/**
 * The organisation management surface, from web's side (ENT-202).
 *
 * Separate from lib/auth/client.ts, and the split is about failure rather than
 * tidiness.
 *
 * That module's `call` returns null on every failure, which is right for what
 * it does: a signed-in person whose bootstrap call failed is still signed in,
 * and turning that into an error page trades a degraded screen for a locked
 * door. Reads want that.
 *
 * Mutations do not. A settings page has to tell "you are not an owner" apart
 * from "core-api is unreachable", because one is a sentence the person needs
 * to read and the other is a retry.
 *
 * This is ENT-198's three-outcome resolution applied to writes. That code
 * distinguishes "not a member" from "the call failed" rather than collapsing
 * both to null, because collapsing them would render a core-api outage as a
 * 404 telling a customer their organisation does not exist. The distinction
 * was designed in rather than learned from an incident, and the same reasoning
 * applies here: null for two different reasons is a lie by omission.
 *
 * So this module keeps the reason. Three outcomes, never two.
 */
import { ORG_HEADER } from '@/lib/auth/client'

/** What went wrong, in terms a page can act on. */
export type Failure =
  /** The caller is authenticated but not allowed. Show the reason. */
  | { kind: 'denied'; message: string }
  /** The thing addressed is not there, or not theirs to see. */
  | { kind: 'missing'; message: string }
  /** A rule refused it, such as removing the last owner. Show the reason. */
  | { kind: 'refused'; message: string }
  /** Anything else: core-api down, a network fault, a bug. Offer a retry. */
  | { kind: 'unavailable'; message: string }

export type Result<T> = { ok: true; value: T } | { ok: false; error: Failure }

export interface Member {
  userId: string
  role: string
  displayName?: string
  email?: string
  joinedAt?: string
}

export interface Organisation {
  orgId: string
  name: string
  slug: string
}

interface Options {
  accessToken: string
  orgId: string
  body?: unknown
}

/**
 * Connect's error shape over HTTP: a JSON body carrying a code and a message.
 *
 * The codes are mapped rather than passed through, because a page should be
 * switching on what it can do about a failure, not on the transport's
 * vocabulary. `failed_precondition` is a rule saying no, which reads
 * identically to `permission_denied` in a toast and completely differently in
 * a decision about whether to offer a retry.
 */
function failureFrom(status: number, code: string, message: string): Failure {
  switch (code) {
    case 'permission_denied':
      return { kind: 'denied', message }
    case 'not_found':
      return { kind: 'missing', message }
    case 'failed_precondition':
    case 'invalid_argument':
      return { kind: 'refused', message }
    default:
      return {
        kind: 'unavailable',
        message: message || `core-api answered ${status}`,
      }
  }
}

async function mutate<T>(method: string, options: Options): Promise<Result<T>> {
  const base = process.env.KINDLAST_CORE_API_URL?.replace(/\/+$/, '')
  if (!base) {
    return {
      ok: false,
      error: { kind: 'unavailable', message: 'core-api is not configured' },
    }
  }

  try {
    const response = await fetch(`${base}/${method}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${options.accessToken}`,
        [ORG_HEADER]: options.orgId,
      },
      body: JSON.stringify(options.body ?? {}),
      cache: 'no-store',
    })

    if (response.ok) {
      return { ok: true, value: (await response.json()) as T }
    }

    // A Connect error body is JSON, but a proxy or an edge returning 502 will
    // not be. Falling back rather than throwing keeps a gateway failure
    // reported as unavailable instead of as a parse error nobody can act on.
    let code = ''
    let message = ''
    try {
      const body = (await response.json()) as {
        code?: string
        message?: string
      }
      code = body.code ?? ''
      message = body.message ?? ''
    } catch {
      // Left as unavailable below.
    }

    return { ok: false, error: failureFrom(response.status, code, message) }
  } catch (error) {
    console.error(`core-api ${method}`, error)
    return {
      ok: false,
      error: { kind: 'unavailable', message: 'core-api is unreachable' },
    }
  }
}

export function listMembers(
  accessToken: string,
  orgId: string,
): Promise<Result<{ members?: Member[] }>> {
  return mutate('kindlast.core.v1.OrgService/ListMembers', {
    accessToken,
    orgId,
  })
}

export function updateMemberRole(
  accessToken: string,
  orgId: string,
  userId: string,
  role: string,
): Promise<Result<{ member?: Member }>> {
  return mutate('kindlast.core.v1.OrgService/UpdateMemberRole', {
    accessToken,
    orgId,
    body: { userId, role },
  })
}

export function removeMember(
  accessToken: string,
  orgId: string,
  userId: string,
): Promise<Result<Record<string, never>>> {
  return mutate('kindlast.core.v1.OrgService/RemoveMember', {
    accessToken,
    orgId,
    body: { userId },
  })
}

export function renameOrganisation(
  accessToken: string,
  orgId: string,
  name: string,
): Promise<Result<Organisation>> {
  return mutate('kindlast.core.v1.OrgService/UpdateOrganisation', {
    accessToken,
    orgId,
    body: { name },
  })
}
