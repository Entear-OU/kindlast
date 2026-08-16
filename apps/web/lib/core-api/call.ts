/**
 * Talking to core-api, and keeping the reason when it says no.
 *
 * Extracted from lib/org/client.ts (ENT-202) when the feed became the second
 * caller (ENT-203). The reasoning it was written with is unchanged and worth
 * keeping in front of whoever adds the third:
 *
 * lib/auth/client.ts returns null on every failure, which is right for what it
 * does. A signed-in person whose bootstrap call failed is still signed in, and
 * turning that into an error page trades a degraded screen for a locked door.
 *
 * Anything a person acts on wants more than that. A settings page has to tell
 * "you are not an owner" apart from "core-api is unreachable", because one is a
 * sentence to read and the other is a retry. Collapsing both to null would
 * render an outage as a 404 telling a customer their organisation does not
 * exist.
 *
 * So: three outcomes, never two.
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
  /**
   * The plan does not cover it (ENT-203).
   *
   * Separate from `denied` because the two are different sentences with
   * different buttons under them. "You are not an owner" sends someone to a
   * colleague; "this needs the Pro plan" sends them to billing. Rendering a
   * payment wall as a permissions error is how a product loses an upgrade it
   * had already earned.
   */
  | { kind: 'payment'; message: string }
  /** Anything else: core-api down, a network fault, a bug. Offer a retry. */
  | { kind: 'unavailable'; message: string }

export type Result<T> = { ok: true; value: T } | { ok: false; error: Failure }

interface Options {
  accessToken: string
  orgId: string
  body?: unknown
}

/**
 * Connect's error shape over HTTP: a JSON body carrying a code and a message.
 *
 * The codes are mapped rather than passed through, because a page should switch
 * on what it can do about a failure, not on the transport's vocabulary.
 * `failed_precondition` is a rule saying no, which reads identically to
 * `permission_denied` in a toast and completely differently in a decision about
 * whether to offer a retry.
 *
 * `resource_exhausted` is the plan gate. Connect has no 402, so the handler
 * uses the nearest code and the meaning is recovered here (§0.5).
 */
export function failureFrom(
  status: number,
  code: string,
  message: string,
): Failure {
  switch (code) {
    case 'permission_denied':
      return { kind: 'denied', message }
    case 'not_found':
      return { kind: 'missing', message }
    case 'failed_precondition':
    case 'invalid_argument':
      return { kind: 'refused', message }
    case 'resource_exhausted':
      return { kind: 'payment', message }
    default:
      return {
        kind: 'unavailable',
        message: message || `core-api answered ${status}`,
      }
  }
}

/** POSTs a Connect procedure and keeps the reason it failed. */
export async function call<T>(
  method: string,
  options: Options,
): Promise<Result<T>> {
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
