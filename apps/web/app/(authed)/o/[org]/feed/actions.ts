'use server'

import { revalidatePath } from 'next/cache'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { FindingActionState } from '@/lib/findings/action-state'
import {
  approveFinding,
  rejectFinding,
  snoozeFinding,
  type Failure,
} from '@/lib/findings/client'

/**
 * The act path, from web's side (ENT-203).
 *
 * Every action re-resolves the organisation from the slug rather than trusting
 * an id posted by the form, which is the whole security story of this file. A
 * hidden field carrying an org id is a field an attacker can edit; the slug in
 * the URL is resolved against the caller's own memberships, and core-api
 * verifies the resulting header again. The form supplies which finding to act
 * on, never which organisation to act in.
 *
 * Note what is NOT posted: who is approving. The acting user comes from the
 * session GUC core-api sets from the verified token, so there is no field for a
 * form to carry and nothing for anyone to tamper with.
 *
 * These return a message rather than throwing. A refused approval is not an
 * exception, it is a sentence to read, and Next's error boundary would replace
 * the finding the person was reading with an apology.
 */

/** Turns a Failure into something worth showing a person. */
function say(error: Failure): FindingActionState {
  switch (error.kind) {
    case 'denied':
      return {
        status: 'error',
        message:
          'Your session is not authorised to act on findings. This is a known gap in sign-in (ENT-221), not a permission an owner can grant.',
      }
    case 'payment':
      // Deliberately different words from `denied`. One sends you to a
      // colleague, the other to billing, and telling them apart is the whole
      // reason the plan gate uses its own code.
      return {
        status: 'error',
        message: 'Acting on findings needs the Pro plan.',
      }
    case 'missing':
      return {
        status: 'error',
        message: 'That finding is no longer here.',
      }
    case 'refused':
      return { status: 'error', message: error.message }
    default:
      return {
        status: 'error',
        message: 'core-api is unreachable. Nothing was changed.',
      }
  }
}

/**
 * Either the credentials to act with, or the sentence explaining why not.
 *
 * A discriminated union rather than null, so a caller cannot forget the failing
 * branch: `'error' in ctx` narrows, and using `ctx.accessToken` without
 * checking does not compile.
 */
type Context =
  { accessToken: string; orgId: string } | { error: FindingActionState }

/** Resolves the caller and the organisation, or explains why it cannot. */
async function context(slug: string): Promise<Context> {
  const session = await currentSession()
  if (!session) {
    return {
      error: {
        status: 'error',
        message: 'Your session has expired. Sign in and try again.',
      } satisfies FindingActionState,
    }
  }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') {
    return {
      error: {
        status: 'error',
        message:
          resolved.status === 'not-a-member'
            ? 'You are not a member of this organisation.'
            : 'core-api is unreachable. Nothing was changed.',
      } satisfies FindingActionState,
    }
  }

  return {
    accessToken: session.accessToken,
    orgId: resolved.membership.orgId,
  }
}

export async function approve(
  _previous: FindingActionState,
  form: FormData,
): Promise<FindingActionState> {
  const slug = String(form.get('slug') ?? '')
  const findingId = String(form.get('findingId') ?? '')
  // Checked rather than assumed: "approved" and "approved having read the
  // regulation" are different claims and the audit trail records which.
  const reviewed = form.get('reviewed') === 'on'

  const ctx = await context(slug)
  if ('error' in ctx) return ctx.error

  const result = await approveFinding(
    ctx.accessToken,
    ctx.orgId,
    findingId,
    reviewed,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/feed'))
  revalidatePath(orgPath(slug, `/feed/${findingId}`))

  // applied:false is not a failure. It means the finding was already approved,
  // which is what a double submit or a browser retry looks like, and telling
  // someone their action failed when the outcome they wanted is already true
  // would send them to look for a problem that is not there.
  if (!result.value.applied) {
    return { status: 'ok', message: 'This finding was already approved.' }
  }

  return {
    status: 'ok',
    message: 'Approved.',
    createdRecordId: result.value.createdRecordId,
    createdRecordTable: result.value.createdRecordTable,
  }
}

export async function reject(
  _previous: FindingActionState,
  form: FormData,
): Promise<FindingActionState> {
  const slug = String(form.get('slug') ?? '')
  const findingId = String(form.get('findingId') ?? '')
  const reason = String(form.get('reason') ?? '')

  const ctx = await context(slug)
  if ('error' in ctx) return ctx.error

  const result = await rejectFinding(
    ctx.accessToken,
    ctx.orgId,
    findingId,
    reason,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/feed'))
  revalidatePath(orgPath(slug, `/feed/${findingId}`))

  return result.value.applied
    ? { status: 'ok', message: 'Rejected.' }
    : { status: 'ok', message: 'This finding was already rejected.' }
}

export async function snooze(
  _previous: FindingActionState,
  form: FormData,
): Promise<FindingActionState> {
  const slug = String(form.get('slug') ?? '')
  const findingId = String(form.get('findingId') ?? '')
  const days = Number(form.get('days') ?? 7)

  const ctx = await context(slug)
  if ('error' in ctx) return ctx.error

  const result = await snoozeFinding(
    ctx.accessToken,
    ctx.orgId,
    findingId,
    Number.isFinite(days) ? days : 7,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/feed'))
  revalidatePath(orgPath(slug, `/feed/${findingId}`))

  if (!result.value.applied) {
    return { status: 'error', message: 'That finding is no longer here.' }
  }

  return {
    status: 'ok',
    message: result.value.snoozedUntil
      ? `Deferred until ${new Date(result.value.snoozedUntil).toLocaleDateString()}.`
      : 'Deferred.',
  }
}
