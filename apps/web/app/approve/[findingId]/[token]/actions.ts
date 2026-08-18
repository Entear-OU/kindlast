'use server'

import type { ApprovalFromEmailState } from '@/lib/findings/approval-from-email-state'

/**
 * Spend an approve link (§8, ENT-249).
 *
 * The second server action in this app that runs for somebody with no session,
 * and the only one that writes to a customer's compliance record. The
 * delegation is the whole identity claim, which is why core-api stores it
 * hashed, expires it within the hour, spends it once, and binds it to the one
 * finding named alongside it.
 *
 * Both halves travel in the POST body. The finding is in the URL as well, and
 * that is not redundancy for its own sake: core-api refuses a delegation whose
 * binding does not match the finding presented, so a token recovered on its own
 * from a mail relay's log or a truncated URL approves nothing.
 *
 * The reply is deliberately the same sentence for every unusable link. Expired,
 * already used, minted for a different finding, minted for somebody since
 * removed from the organisation, and never real are indistinguishable from
 * here, because distinguishing them would let anyone probe which links are live
 * without proving anything. Somebody who genuinely clicked a stale link is told
 * plainly that it no longer works and where to go instead.
 */
export async function approveFromEmailAction(
  _previous: ApprovalFromEmailState,
  form: FormData,
): Promise<ApprovalFromEmailState> {
  const token = String(form.get('token') ?? '')
  const findingId = String(form.get('findingId') ?? '')
  if (!token || !findingId) {
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
    const response = await fetch(`${base.replace(/\/+$/, '')}/api/v1/approve`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token, findingId }),
      cache: 'no-store',
    })

    if (!response.ok) {
      // 404 covers every unusable link, so this branch cannot and must not say
      // which kind it was.
      return {
        status: 'error',
        message:
          'That link has already been used or has expired. ' +
          'Sign in and approve the finding from your feed instead.',
      }
    }

    const body = (await response.json()) as {
      orgSlug?: string
      applied?: boolean
    }

    // Where to go next comes from the organisation the delegation named, never
    // from anything this request carried. §8's named failure is a consultant
    // with three clients acting against the wrong company from a stale link,
    // and a destination assembled here from a URL segment would be exactly
    // that bug with extra steps.
    const destination = body.orgSlug
      ? `/o/${body.orgSlug}/feed/${findingId}`
      : ''

    // `applied` false means it was already approved: a second click, a retry,
    // or a colleague who got there first. Telling somebody their link failed
    // when the thing they wanted is done would be a lie in the unhelpful
    // direction.
    return {
      status: 'ok',
      message: body.applied
        ? 'Approved. It is recorded as your decision, made from an email.'
        : 'This finding was already approved, so nothing changed.',
      destination,
    }
  } catch {
    // A network failure is not a refused link, and saying so matters: the first
    // is worth retrying and the second never is.
    return {
      status: 'error',
      message: 'Could not reach the service. Try again in a moment.',
    }
  }
}
