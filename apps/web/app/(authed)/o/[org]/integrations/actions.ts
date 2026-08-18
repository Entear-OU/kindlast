'use server'

import { revalidatePath } from 'next/cache'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { ConnectState } from '@/components/integrations/connect-form'
import type { ActionState } from '@/lib/org/action-state'
import {
  connectIntegration,
  discoverIntegration,
  revokeIntegration,
  updateToolGrants,
  type Failure,
  type IntegrationTool,
} from '@/lib/integrations/client'

/**
 * Connecting, granting and revoking (ENT-231, §26.4).
 *
 * # THE ORGANISATION COMES FROM THE SLUG, NEVER FROM THE FORM
 *
 * The same rule every action on this surface follows. A hidden field carrying
 * an org id is a field somebody can edit; the slug in the URL is resolved
 * against the caller's own memberships, and core-api verifies the resulting
 * header again anyway. The form supplies what to connect, never whose
 * organisation to connect it in.
 *
 * # NOTHING HERE FETCHES AN ENDPOINT
 *
 * Discovery goes to core-api, which goes to the gateway, which is the only
 * process that dials an address a customer supplied. Fetching the endpoint
 * from this file would put outbound requests to arbitrary hosts in the Next.js
 * server and route around the egress allow-list entirely, which is worth
 * saying here because it is the obvious shortcut and it would look like a
 * performance improvement.
 */

function say(error: Failure): string {
  switch (error.kind) {
    case 'denied':
      // The two most common causes are a host the operator has not permitted
      // and a tool that is not granted, and core-api's message names which.
      // Replacing it with something vaguer would leave somebody unable to tell
      // a policy decision from a permissions problem.
      return error.message
    case 'missing':
      return 'That connection is not one we hold.'
    case 'refused':
      return error.message
    case 'payment':
      // Nothing on this surface is plan-gated, so this is unreachable today.
      // Handled rather than defaulted because the switch is exhaustive on
      // purpose: a new failure kind should stop this file compiling.
      return error.message
    case 'unavailable':
      return 'Could not reach the service. Try again in a moment.'
  }
}

/**
 * Discover, then connect. One action, two steps, decided by a form field.
 *
 * Two steps rather than one because connecting in a single call would mean
 * somebody agreeing to a tool list they never saw. The first step stores
 * nothing at all, so an endpoint outside the operator's allow-list is refused
 * before there is a row to clean up.
 */
export async function connectAction(
  slug: string,
  _previous: ConnectState,
  form: FormData,
): Promise<ConnectState> {
  const session = await currentSession()
  if (!session) return { status: 'error', message: 'Your session has expired.' }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok')
    return { status: 'error', message: 'Your session has expired.' }

  const displayName = String(form.get('displayName') ?? '').trim()
  const endpointUrl = String(form.get('endpointUrl') ?? '').trim()
  const credential = String(form.get('credential') ?? '').trim()

  if (!displayName) return { status: 'error', message: 'Give it a name.' }
  if (!endpointUrl)
    return { status: 'error', message: 'Say where the endpoint is.' }

  if (String(form.get('step') ?? '') !== 'connect') {
    const discovered = await discoverIntegration(
      session.accessToken,
      resolved.membership.orgId,
      endpointUrl,
      credential || undefined,
    )
    if (!discovered.ok) {
      return {
        status: 'error',
        message: say(discovered.error),
        displayName,
        endpointUrl,
      }
    }

    const tools = discovered.value.tools ?? []
    if (tools.length === 0) {
      return {
        status: 'error',
        message: 'That endpoint offers no tools, so there is nothing to connect.',
        displayName,
        endpointUrl,
      }
    }

    return {
      status: 'ok',
      message:
        'Nothing has been stored yet. Tick what Kindlast may call, then connect.',
      displayName,
      endpointUrl,
      tools,
    }
  }

  const offeredTools = readOfferedTools(form.get('offeredTools'))
  if (!offeredTools) {
    return {
      status: 'error',
      message: 'Start again: the tool list did not survive the round trip.',
      displayName,
      endpointUrl,
    }
  }

  // `getAll`, because a checkbox group posts one entry per ticked box and
  // `get` would silently take the first. A single-value read here would mean a
  // customer ticking four tools and getting one, which is the sort of bug that
  // reads as the product ignoring them.
  const grantedTools = form.getAll('grantedTools').map(String).filter(Boolean)

  const created = await connectIntegration(
    session.accessToken,
    resolved.membership.orgId,
    {
      displayName,
      endpointUrl,
      credential: credential || undefined,
      offeredTools,
      grantedTools,
    },
  )
  if (!created.ok) {
    return {
      status: 'error',
      message: say(created.error),
      displayName,
      endpointUrl,
      tools: offeredTools,
    }
  }

  revalidatePath(orgPath(slug, '/integrations'))

  // Said plainly, including the case where nothing was ticked. A connection
  // with no granted tools is a legitimate thing to create, and a person who
  // did it by accident should find out now rather than the first time a fetch
  // is declined.
  return {
    status: 'ok',
    message:
      grantedTools.length > 0
        ? `Connected. Kindlast may call ${grantedTools.length} of ${offeredTools.length} tools.`
        : 'Connected, and Kindlast may call none of its tools until you allow one.',
  }
}

/**
 * Replace which tools a connection may call.
 *
 * A replace rather than an add-and-remove pair, because two operations invite
 * a client to send one of them. Every grant writes a new consent record, so
 * widening an allow-list leaves the narrower agreement in place to be found.
 */
export async function updateGrantsAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const session = await currentSession()
  if (!session) return { status: 'error', message: 'Your session has expired.' }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok')
    return { status: 'error', message: 'Your session has expired.' }

  const integrationId = String(form.get('integrationId') ?? '')
  if (!integrationId)
    return { status: 'error', message: 'That connection is not one we hold.' }

  const grantedTools = form.getAll('grantedTools').map(String).filter(Boolean)

  const result = await updateToolGrants(
    session.accessToken,
    resolved.membership.orgId,
    integrationId,
    grantedTools,
  )
  if (!result.ok) return { status: 'error', message: say(result.error) }

  revalidatePath(orgPath(slug, '/integrations'))
  return {
    status: 'ok',
    message: 'Recorded, along with what you agreed to and when.',
  }
}

/**
 * Stop using a connection, permanently.
 *
 * The copy says permanently because it is: reconnecting is a new connection
 * with a new consent. Permission to reach somebody's systems should not be
 * resurrectable by a person who did not know why it was withdrawn.
 */
export async function revokeAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const session = await currentSession()
  if (!session) return { status: 'error', message: 'Your session has expired.' }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok')
    return { status: 'error', message: 'Your session has expired.' }

  const integrationId = String(form.get('integrationId') ?? '')
  if (!integrationId)
    return { status: 'error', message: 'That connection is not one we hold.' }

  const result = await revokeIntegration(
    session.accessToken,
    resolved.membership.orgId,
    integrationId,
  )
  if (!result.ok) return { status: 'error', message: say(result.error) }

  revalidatePath(orgPath(slug, '/integrations'))
  return {
    status: 'ok',
    message:
      'Revoked. Kindlast will fetch nothing further from it. What it already ' +
      'read stays in your record.',
  }
}

/**
 * Reads the tool list back off the form.
 *
 * Returns null rather than throwing on anything unexpected, because the
 * failure a person can act on is "start again" and a stack trace is not that.
 *
 * The shape is checked and the flags are carried through as sent. That is safe
 * for the reason the form's own comment gives: a tampered `writeCapable`
 * changes the label a tool is STORED under and not what it may do, because the
 * gateway reads the endpoint's own annotation again before every call and
 * takes the stricter of the two.
 */
function readOfferedTools(raw: FormDataEntryValue | null): IntegrationTool[] | null {
  if (typeof raw !== 'string' || !raw) return null

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!Array.isArray(parsed)) return null

  const tools: IntegrationTool[] = []
  for (const item of parsed) {
    if (typeof item !== 'object' || item === null) return null
    const tool = item as Record<string, unknown>
    if (typeof tool.name !== 'string' || !tool.name) return null
    tools.push({
      name: tool.name,
      description: typeof tool.description === 'string' ? tool.description : '',
      writeCapable: Boolean(tool.writeCapable),
    })
  }
  return tools.length > 0 ? tools : null
}
