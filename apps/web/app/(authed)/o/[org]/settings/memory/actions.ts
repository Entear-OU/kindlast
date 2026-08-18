'use server'

import { revalidatePath } from 'next/cache'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { ActionState } from '@/lib/org/action-state'
import {
  FACT_LABELS,
  correctFact,
  type FactValue,
  type Failure,
  type ProfileFactKey,
} from '@/lib/memory/client'

/**
 * Correcting what Kindlast believes (ENT-228, §26.5).
 *
 * # THE ORGANISATION COMES FROM THE SLUG, NEVER FROM THE FORM
 *
 * The same rule as every other action on this surface, and the whole security
 * story of this file. A hidden field carrying an org id is a field somebody can
 * edit; the slug in the URL is resolved against the caller's own memberships,
 * and core-api verifies the resulting header again anyway. The form supplies
 * what to correct, never whose profile to correct it in.
 *
 * # THIS IS NOT AN EDIT, AND THE MESSAGES SAY SO
 *
 * A correction closes the current value and records a new one. The previous
 * answer survives and is on the history page. So the copy says "recorded" and
 * not "saved": a person told their change was saved reasonably assumes the old
 * one is gone, and here it is not, which is the point.
 */

function say(error: Failure): ActionState {
  switch (error.kind) {
    case 'denied':
      return {
        status: 'error',
        message: 'You do not have permission to change this.',
      }
    case 'missing':
      return { status: 'error', message: 'That is not a fact we hold.' }
    case 'refused':
      // core-api's message is the specific one and is written for a person:
      // "memory: has_dpo holds yes, no or unsure". Passing it through beats
      // replacing it with something vaguer.
      return { status: 'error', message: error.message }
    case 'payment':
      // Nothing on this surface is plan-gated, so this is unreachable today.
      // Handled rather than defaulted because the switch is exhaustive on
      // purpose: a new failure kind should stop this file compiling.
      return { status: 'error', message: error.message }
    case 'unavailable':
      return {
        status: 'error',
        message: 'Could not reach the service. Try again in a moment.',
      }
  }
}

/**
 * Reads the posted value into the shape its key takes.
 *
 * The form cannot decide this, and that is deliberate. Which arm a fact takes
 * is part of what the fact IS, so the mapping lives here beside the key list
 * rather than in a hidden field the browser sends. A hidden field would make
 * the shape something a caller asserts, and core-api would then be refusing
 * malformed patches that a correct client could never send.
 */
function readValue(key: ProfileFactKey, raw: string): FactValue | string {
  const trimmed = raw.trim()

  switch (key) {
    case 'PROFILE_FACT_KEY_HAS_DPO':
    case 'PROFILE_FACT_KEY_HAS_ROPA':
    case 'PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU':
    case 'PROFILE_FACT_KEY_HIGH_RISK_PROCESSING':
    case 'PROFILE_FACT_KEY_HIGH_RISK_AI_SYSTEM':
    case 'PROFILE_FACT_KEY_LARGE_SCALE_MONITORING':
      switch (trimmed) {
        case 'yes':
          return { triState: 'TRI_STATE_YES' }
        case 'no':
          return { triState: 'TRI_STATE_NO' }
        case 'unsure':
          // A real answer rather than a missing one, which is why it is an
          // option on the form and not the absence of one.
          return { triState: 'TRI_STATE_UNSURE' }
        default:
          return 'Choose yes, no, or not sure.'
      }

    case 'PROFILE_FACT_KEY_STAFF_COUNT': {
      if (!/^\d+$/.test(trimmed)) return 'Staff should be a whole number.'
      return { number: trimmed }
    }

    case 'PROFILE_FACT_KEY_INDUSTRY': {
      if (!trimmed) return 'Say what your industry is.'
      return { text: trimmed }
    }

    default: {
      // The list-valued facts. An empty box is an empty list rather than an
      // error, because "we operate no AI systems" is an answer somebody needs
      // to be able to give, and refusing it would leave them unable to say the
      // one thing that clears an obligation.
      const values = trimmed
        ? trimmed
            .split(',')
            .map((item) => item.trim())
            .filter(Boolean)
        : []
      return { list: { values } }
    }
  }
}

export async function correctFactAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const session = await currentSession()
  if (!session) return { status: 'error', message: 'Your session has expired.' }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok')
    return { status: 'error', message: 'Your session has expired.' }

  const key = String(form.get('key') ?? '')
  if (!(key in FACT_LABELS))
    return { status: 'error', message: 'That is not a fact we hold.' }
  const factKey = key as ProfileFactKey

  const value = readValue(factKey, String(form.get('value') ?? ''))
  if (typeof value === 'string') return { status: 'error', message: value }

  const note = String(form.get('note') ?? '').trim()

  const result = await correctFact(
    session.accessToken,
    resolved.membership.orgId,
    factKey,
    value,
    note || undefined,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/settings/memory'))
  revalidatePath(orgPath(slug, `/settings/memory/${key}`))

  // `changed: false` is reported rather than swallowed. A person who
  // re-submitted a form deserves to know nothing moved, and silently claiming
  // a correction would put a row in a history that has none.
  if (!result.value.changed) {
    return {
      status: 'ok',
      message: `${FACT_LABELS[factKey]} already said that, so nothing changed.`,
    }
  }
  return {
    status: 'ok',
    message: `Recorded. The previous answer is kept in this fact's history.`,
  }
}
