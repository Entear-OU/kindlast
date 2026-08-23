'use server'

import { revalidatePath } from 'next/cache'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { ActionState } from '@/lib/org/action-state'
import { FACT_LABELS, type ProfileFactKey } from '@/lib/memory/client'
import {
  answerQuestion,
  startOnboarding,
  type Failure,
} from '@/lib/onboarding/client'

/**
 * The interview's writes (ENT-212, ENT-254).
 *
 * # THE ORGANISATION COMES FROM THE SLUG, NEVER FROM THE FORM
 *
 * The same rule as every other action in the console. A hidden field carrying
 * an org id is a field somebody can edit; the slug in the URL is resolved
 * against the caller's own memberships, and core-api verifies the resulting
 * header again anyway. The form supplies what was answered, never whose profile
 * to answer for.
 *
 * # NOTHING HERE PARSES AN ANSWER
 *
 * What the person typed goes to core-api verbatim, and core-api decides whether
 * it is a list of three countries, a number, or something it will not accept. A
 * console that parsed would be a second implementation of the rule that decides
 * what a customer's profile contains, and the day the two disagree the profile
 * holds something nobody typed. So a refusal comes back as a sentence and is
 * shown as it was written.
 */

function say(error: Failure): ActionState {
  switch (error.kind) {
    case 'denied':
      return {
        status: 'error',
        message: 'You do not have permission to do this.',
      }
    case 'missing':
      return { status: 'error', message: 'That question is not one we ask.' }
    case 'refused':
      // core-api's sentence is the specific one and is written for a person to
      // read: "answer that one with yes, no or unsure". Replacing it with
      // something vaguer would lose the only part that helps.
      return { status: 'error', message: error.message }
    case 'payment':
      // Nothing here is plan-gated. Handled rather than defaulted, so a new
      // failure kind stops this file compiling instead of falling through.
      return { status: 'error', message: error.message }
    case 'unavailable':
      return {
        status: 'error',
        message: 'Could not reach the service. Try again in a moment.',
      }
  }
}

async function acting(slug: string) {
  const session = await currentSession()
  if (!session) return null

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') return null

  return { token: session.accessToken, orgId: resolved.membership.orgId }
}

const expired: ActionState = {
  status: 'error',
  message: 'Your session has expired.',
}

export async function startOnboardingAction(
  slug: string,
  _previous: ActionState,
  _form: FormData,
): Promise<ActionState> {
  const who = await acting(slug)
  if (!who) return expired

  const result = await startOnboarding(who.token, who.orgId)
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/onboarding'))
  return { status: 'ok', message: '' }
}

export async function answerQuestionAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const who = await acting(slug)
  if (!who) return expired

  const key = String(form.get('key') ?? '')
  if (!(key in FACT_LABELS))
    return { status: 'error', message: 'That question is not one we ask.' }

  // A skip is a decision and an empty box is a mistake, so they are different
  // submissions rather than the same one. Collapsing them would record a
  // deliberate refusal every time somebody hit enter too early.
  const skip = form.get('skip') === 'true'
  const answer = String(form.get('answer') ?? '')

  const result = await answerQuestion(
    who.token,
    who.orgId,
    key as ProfileFactKey,
    answer,
    skip,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/onboarding'))

  // EVERYTHING THE PROFILE UNBLOCKS, ON THE ANSWER THAT WROTE IT (ENT-254).
  //
  // This used to sit behind a confirm button, which made it easy to see when
  // it had to run. Now the last answer writes the profile, so the dashboard
  // stops saying "nothing to check yet", the feed fills, the record registers
  // start accepting writes and the memory page stops saying we know nothing,
  // all on one ordinary answer. None of them would notice without being told,
  // and there is no longer a second call to tell them.
  if (result.value.state?.profileExists) {
    revalidatePath(orgPath(slug))
    revalidatePath(orgPath(slug, '/feed'))
    revalidatePath(orgPath(slug, '/settings/memory'))
  }

  return { status: 'ok', message: '' }
}
