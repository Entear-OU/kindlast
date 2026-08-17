'use server'

import type { ActionState } from '@/lib/org/action-state'

/**
 * Spend an unsubscribe token (ENT-209).
 *
 * Unauthenticated on purpose, which makes it the one server action in this app
 * that runs for somebody with no session. The token is the only identity claim,
 * which is why core-api stores it hashed, expires it, and spends it once.
 *
 * The reply is deliberately the same sentence for every unusable token: expired,
 * already used, wrong kind and never existed are indistinguishable from here,
 * because distinguishing them would let anyone probe which tokens are real
 * without proving anything. A person who genuinely clicked a stale link is told
 * plainly that it no longer works and where to go instead.
 */
export async function unsubscribeAction(
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const token = String(form.get('token') ?? '')
  if (!token) {
    return { status: 'error', message: 'That link is not valid.' }
  }

  const base = process.env.KINDLAST_CORE_API_URL
  if (!base) {
    return {
      status: 'error',
      message: 'Could not reach the service. Try again in a moment.',
    }
  }

  try {
    const response = await fetch(
      `${base.replace(/\/+$/, '')}/api/v1/unsubscribe`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
        cache: 'no-store',
      },
    )

    if (!response.ok) {
      // 404 covers every unusable token, so this branch cannot and must not
      // say which kind it was.
      return {
        status: 'error',
        message:
          'That link has already been used or has expired. ' +
          'You can change your notification settings from the settings page.',
      }
    }

    return {
      status: 'ok',
      message:
        'Done. You will not get the weekly briefing or deadline alerts for ' +
        'this organisation, and only critical findings will be emailed.',
    }
  } catch {
    // A network failure is not the same as a refused token, and saying so
    // matters: the first is worth retrying and the second never is.
    return {
      status: 'error',
      message: 'Could not reach the service. Try again in a moment.',
    }
  }
}
