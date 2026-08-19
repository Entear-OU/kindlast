'use server'

import { revalidatePath } from 'next/cache'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { ActionState } from '@/lib/org/action-state'
import {
  getModelSetting,
  chooseBundledModel,
  chooseHostedModel,
  type Failure,
  type ModelSettingView,
} from '@/lib/model/client'

/**
 * Changing where this organisation's model runs (ENT-236, §26.6).
 *
 * # THE KEY PASSES THROUGH AND IS NOT KEPT
 *
 * It arrives in a FormData, goes straight to core-api, and is never returned,
 * logged, or put in an `ActionState`. The success message names the last four
 * characters at most, because the message is rendered into a page and a page is
 * something people screenshot.
 *
 * # THE ACKNOWLEDGEMENT IS THE PERSON'S, NOT THIS FILE'S
 *
 * `acknowledgeConsequence` is read from the form rather than hard-coded true.
 * That looks like a formality and is not: hard-coding it would mean the console
 * asserting on somebody's behalf that they were told, and core-api's refusal
 * without it is the only thing making the warning unskippable. If the checkbox
 * is ever removed from the form, this must start failing rather than start
 * working.
 */

function say(error: Failure): ActionState {
  switch (error.kind) {
    case 'denied':
      return {
        status: 'error',
        message:
          'Only an owner can change where this organisation is processed.',
      }
    case 'missing':
      return { status: 'error', message: 'That setting no longer exists.' }
    case 'refused':
      // core-api's own sentence, which for this surface is the specific and
      // useful one: which provider is not permitted, or which part of the
      // endpoint failed the check. Replacing it with something vaguer would
      // leave an owner guessing at an operator's allow-list.
      return { status: 'error', message: error.message }
    case 'payment':
      return { status: 'error', message: error.message }
    case 'unavailable':
      return {
        status: 'error',
        message: 'Could not reach the service. Try again in a moment.',
      }
  }
}

async function context(slug: string) {
  const session = await currentSession()
  if (!session) return null

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') return null

  return {
    accessToken: session.accessToken,
    orgId: resolved.membership.orgId,
    role: resolved.membership.role,
  }
}

/** What is serving this organisation, and what the deployment permits. */
export async function modelSettingFor(
  accessToken: string,
  orgId: string,
): Promise<ModelSettingView | null> {
  const result = await getModelSetting(accessToken, orgId)
  // Null rather than a default view. An organisation shown "uses the bundled
  // model" because the call failed would be told the safest possible thing
  // about a situation nobody checked, which is the one wrong answer on this
  // page that nobody would think to question.
  return result.ok ? result.value : null
}

export async function chooseHostedModelAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const ctx = await context(slug)
  if (!ctx) return { status: 'error', message: 'Your session has expired.' }

  const provider = String(form.get('provider') ?? '').trim()
  const baseUrl = String(form.get('baseUrl') ?? '').trim()
  const model = String(form.get('model') ?? '').trim()
  const apiKey = String(form.get('apiKey') ?? '')
  const acknowledged = form.get('acknowledge') === 'on'

  if (!provider || !baseUrl || !model) {
    return {
      status: 'error',
      message: 'A provider, an endpoint and a model are all needed.',
    }
  }
  if (!acknowledged) {
    // Answered here as well as by core-api, and the duplication is on purpose:
    // this one is a sentence beside the checkbox, and core-api's is the control.
    // Removing this changes the wording somebody sees; removing core-api's
    // would change what the product permits.
    return {
      status: 'error',
      message:
        'Confirm you understand what changes before this can be turned on.',
    }
  }

  const result = await chooseHostedModel(ctx.accessToken, ctx.orgId, {
    provider,
    baseUrl,
    model,
    apiKey: apiKey || undefined,
    acknowledgeConsequence: true,
  })
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/settings/model'))
  revalidatePath(orgPath(slug, '/logs'))
  return {
    status: 'ok',
    message: `Recorded. Findings for this organisation are now drafted by ${provider}, and the change is in your audit log.`,
  }
}

export async function chooseBundledModelAction(
  slug: string,
  _previous: ActionState,
): Promise<ActionState> {
  const ctx = await context(slug)
  if (!ctx) return { status: 'error', message: 'Your session has expired.' }

  const result = await chooseBundledModel(ctx.accessToken, ctx.orgId)
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/settings/model'))
  revalidatePath(orgPath(slug, '/logs'))

  // Two sentences, and the second one is the one that matters. An owner who
  // read only "turned off" could reasonably tell a regulator that nothing left,
  // which would be untrue for the period before they clicked.
  return {
    status: 'ok',
    message: result.value.auditEntryId
      ? 'Turned off and recorded. Nothing further leaves this deployment, and the stored key is destroyed. Content the provider already processed stays with them.'
      : 'This organisation was already using the model this deployment runs.',
  }
}
