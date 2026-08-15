'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import {
  idle,
  removeMemberAction,
  updateMemberRoleAction,
  type ActionState,
} from '@/app/(authed)/o/[org]/settings/actions'
import type { Member } from '@/lib/org/client'

const ROLES = ['owner', 'member', 'viewer'] as const

/**
 * The member list (ENT-202).
 *
 * Owner-only controls are hidden from everyone else, and that is presentation
 * rather than protection. The server refuses the write regardless: scope,
 * then the handler's role check, then RLS. Hiding a control a person cannot
 * use is politeness; relying on hiding it would be the mistake.
 *
 * A viewer therefore sees the list and no buttons, which is exactly what
 * ListMembers declaring `org:read` rather than `org:manage` is for.
 */
export function MembersTable({
  slug,
  members,
  viewerRole,
}: {
  slug: string
  members: Member[]
  viewerRole: string
}) {
  const canManage = viewerRole === 'owner'

  return (
    <table className="w-full text-left text-sm">
      <thead>
        <tr className="border-b border-border/60">
          <th className="pb-2 font-medium text-muted-foreground">Person</th>
          <th className="pb-2 font-medium text-muted-foreground">Role</th>
          {canManage ? <th className="pb-2 sr-only">Actions</th> : null}
        </tr>
      </thead>
      <tbody>
        {members.map((member) => (
          <MemberRow
            key={member.userId}
            slug={slug}
            member={member}
            canManage={canManage}
          />
        ))}
      </tbody>
    </table>
  )
}

function MemberRow({
  slug,
  member,
  canManage,
}: {
  slug: string
  member: Member
  canManage: boolean
}) {
  const [roleState, changeRole] = useActionState<ActionState, FormData>(
    updateMemberRoleAction.bind(null, slug),
    idle,
  )
  const [removeState, remove] = useActionState<ActionState, FormData>(
    removeMemberAction.bind(null, slug),
    idle,
  )

  const problem =
    roleState.status === 'error'
      ? roleState.message
      : removeState.status === 'error'
        ? removeState.message
        : ''

  return (
    <tr className="border-b border-border/40 align-middle">
      <td className="py-3">
        {/* Falls back through name, then address, then id. Any of the three
            can be missing: display_name is absent when the authorization
            server returned no name claim, and both are absent for someone
            invited who has not yet signed in. An id is ugly and is still
            better than an empty cell nobody can act on. */}
        <span className="text-foreground">
          {member.displayName || member.email || member.userId}
        </span>
        {member.displayName && member.email ? (
          <span className="ml-2 text-muted-foreground">{member.email}</span>
        ) : null}
        {problem ? (
          <p role="alert" className="mt-1 text-xs text-destructive">
            {problem}
          </p>
        ) : null}
      </td>

      <td className="py-3">
        {canManage ? (
          <form action={changeRole} className="flex items-center gap-2">
            <input type="hidden" name="userId" value={member.userId} />
            <label className="sr-only" htmlFor={`role-${member.userId}`}>
              Role for {member.displayName || member.email || member.userId}
            </label>
            <select
              id={`role-${member.userId}`}
              name="role"
              defaultValue={member.role}
              className="rounded-md border border-border/60 bg-background px-2 py-1 text-sm"
            >
              {ROLES.map((role) => (
                <option key={role} value={role}>
                  {role}
                </option>
              ))}
            </select>
            <Button type="submit" variant="outline" size="sm">
              Save
            </Button>
          </form>
        ) : (
          <span className="text-muted-foreground">{member.role}</span>
        )}
      </td>

      {canManage ? (
        <td className="py-3 text-right">
          <form action={remove}>
            <input type="hidden" name="userId" value={member.userId} />
            <Button type="submit" variant="ghost" size="sm">
              Remove
            </Button>
          </form>
        </td>
      ) : null}
    </tr>
  )
}
