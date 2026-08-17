'use server'

import { revalidatePath } from 'next/cache'

import { resolveOrg, orgPath } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { ActionState } from '@/lib/org/action-state'
import {
  inviteMember,
  listMembers,
  removeMember,
  renameOrganisation,
  updateMemberRole,
  type Failure,
  type Member,
} from '@/lib/org/client'

/**
 * The settings surface's writes (ENT-202).
 *
 * Every action re-resolves the organisation from the slug rather than trusting
 * an id posted by the form, and that is the whole security story of this file.
 * A hidden field carrying an org id is a field an attacker can edit; the slug
 * in the URL is resolved against the caller's own memberships, and core-api
 * verifies the resulting header again anyway. The form supplies what to change,
 * never which organisation to change it in.
 *
 * These return a message rather than throwing. A failed rename is not an
 * exception, it is a sentence the person needs to read, and Next's error
 * boundary would replace the page they were working on with an apology.
 */

/** Turns a Failure into something worth showing a person. */
function say(error: Failure): ActionState {
  switch (error.kind) {
    case 'denied':
      return {
        status: 'error',
        message: 'Only an owner can do that.',
      }
    case 'missing':
      return {
        status: 'error',
        message: 'That person is no longer in this organisation.',
      }
    case 'refused':
      // core-api's message is the specific one, and it is written for a
      // person: "an organisation must keep at least one owner". Passing it
      // through beats replacing it with something vaguer.
      return { status: 'error', message: error.message }
    case 'payment':
      // No endpoint on this surface is plan-gated, so this is unreachable
      // today. It is handled rather than defaulted because the switch is
      // exhaustive on purpose: adding `payment` to the Failure union (ENT-203)
      // made this function stop compiling, which is exactly the reminder a new
      // failure kind should produce in every place that renders one.
      return { status: 'error', message: error.message }
    case 'unavailable':
      return {
        status: 'error',
        message: 'Could not reach the service. Try again in a moment.',
      }
  }
}

/**
 * Resolves the caller and the organisation the slug names.
 *
 * Returns null when there is nothing to act in, which the callers turn into an
 * error rather than a redirect: an action is a background request, and
 * redirecting one leaves the person looking at an unchanged page wondering
 * whether they clicked it.
 */
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

export async function renameOrganisationAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const ctx = await context(slug)
  if (!ctx) return { status: 'error', message: 'Your session has expired.' }

  const name = String(form.get('name') ?? '').trim()
  if (!name) {
    return { status: 'error', message: 'An organisation needs a name.' }
  }

  const result = await renameOrganisation(ctx.accessToken, ctx.orgId, name)
  if (!result.ok) return say(result.error)

  // The slug is unchanged by a rename, so the path being revalidated is still
  // the path the person is on. That is worth knowing rather than assuming: if
  // renaming did move the slug, this would silently revalidate a page that no
  // longer exists.
  revalidatePath(orgPath(slug, '/settings'))
  return { status: 'ok', message: `Renamed to ${result.value.name}.` }
}

export async function updateMemberRoleAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const ctx = await context(slug)
  if (!ctx) return { status: 'error', message: 'Your session has expired.' }

  const userId = String(form.get('userId') ?? '')
  const role = String(form.get('role') ?? '')

  const result = await updateMemberRole(
    ctx.accessToken,
    ctx.orgId,
    userId,
    role,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/settings'))
  return { status: 'ok', message: 'Role updated.' }
}

export async function removeMemberAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const ctx = await context(slug)
  if (!ctx) return { status: 'error', message: 'Your session has expired.' }

  const userId = String(form.get('userId') ?? '')

  const result = await removeMember(ctx.accessToken, ctx.orgId, userId)
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/settings'))
  return { status: 'ok', message: 'Removed from the organisation.' }
}

/**
 * Invite somebody to this organisation (ENT-219).
 *
 * The success message says the email was sent rather than that the invitation
 * was created, because as of ENT-219 those are the same transaction: core-api
 * writes the message onto the outbox alongside the invitation, so a successful
 * call means the email exists and is queued. Saying "created" would be true and
 * would leave the owner wondering whether to tell the person separately.
 *
 * It does not promise the message has arrived, because the dispatcher runs on a
 * timer and the mail server may be down. "On its way" is the honest tense.
 */
export async function inviteMemberAction(
  slug: string,
  _previous: ActionState,
  form: FormData,
): Promise<ActionState> {
  const ctx = await context(slug)
  if (!ctx) return { status: 'error', message: 'Your session has expired.' }

  const email = String(form.get('email') ?? '').trim()
  const role = String(form.get('role') ?? 'member')

  if (!email) {
    // Answered here rather than by the server, only because a round trip to be
    // told a blank field is blank is a poor use of a person's attention. The
    // real validation is still core-api's; this is not a boundary.
    return { status: 'error', message: 'Enter an email address to invite.' }
  }

  const result = await inviteMember(ctx.accessToken, ctx.orgId, email, role)
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/settings'))
  return { status: 'ok', message: `Invitation on its way to ${email}.` }
}

/** The member list, or null when it could not be read. */
export async function membersOf(
  accessToken: string,
  orgId: string,
): Promise<Member[] | null> {
  const result = await listMembers(accessToken, orgId)
  if (!result.ok) return null
  return result.value.members ?? []
}
