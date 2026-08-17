/**
 * The organisation management surface, from web's side (ENT-202).
 *
 * The transport and the failure vocabulary moved to lib/core-api/call.ts when
 * the feed became a second caller (ENT-203). The reasoning behind the
 * three-outcome shape lives there; this module is now just the organisation's
 * procedures.
 *
 * Failure and Result are re-exported rather than re-declared so that callers
 * and tests written against this module keep working, and so there is one
 * definition of what a failure is rather than two that can drift.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

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

export function listMembers(accessToken: string, orgId: string) {
  return call<{ members?: Member[] }>(
    'kindlast.core.v1.OrgService/ListMembers',
    { accessToken, orgId },
  )
}

export function updateMemberRole(
  accessToken: string,
  orgId: string,
  userId: string,
  role: string,
) {
  return call<{ member?: Member }>(
    'kindlast.core.v1.OrgService/UpdateMemberRole',
    { accessToken, orgId, body: { userId, role } },
  )
}

export function removeMember(
  accessToken: string,
  orgId: string,
  userId: string,
) {
  return call<Record<string, never>>(
    'kindlast.core.v1.OrgService/RemoveMember',
    { accessToken, orgId, body: { userId } },
  )
}

export interface Invitation {
  invitationId: string
  expiresAt?: string
}

/**
 * Invite somebody by email address.
 *
 * The response deliberately never carries the token: the raw value exists for
 * the life of core-api's handler, which writes the email into the transactional
 * outbox in the same transaction, and only its hash is stored (ENT-219). So
 * there is nothing here for the console to display or resend, and that is the
 * point rather than a limitation.
 */
export function inviteMember(
  accessToken: string,
  orgId: string,
  email: string,
  role: string,
) {
  return call<Invitation>('kindlast.core.v1.OrgService/InviteMember', {
    accessToken,
    orgId,
    body: { email, role },
  })
}

export function renameOrganisation(
  accessToken: string,
  orgId: string,
  name: string,
) {
  return call<Organisation>('kindlast.core.v1.OrgService/UpdateOrganisation', {
    accessToken,
    orgId,
    body: { name },
  })
}
