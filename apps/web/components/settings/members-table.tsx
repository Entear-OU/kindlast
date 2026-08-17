'use client'

import { useActionState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  removeMemberAction,
  updateMemberRoleAction,
} from '@/app/(authed)/o/[org]/settings/actions'
import { idle, type ActionState } from '@/lib/org/action-state'
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
 *
 * `viewerUserId` is core-api's derived id for the person reading the page, not
 * the IdP subject claim (ENT-220). Comparing the wrong one silently matches
 * nothing, so every row would look like somebody else and leaving would
 * disappear again.
 */
export function MembersTable({
  slug,
  members,
  viewerRole,
  viewerUserId,
}: {
  slug: string
  members: Member[]
  viewerRole: string
  viewerUserId?: string
}) {
  const canManage = viewerRole === 'owner'
  // Every role may leave: `memberships_delete_owner_or_self` has always allowed
  // removing yourself, so the action column is no longer owner-only.
  const showActions =
    canManage || members.some((m) => m.userId === viewerUserId)

  return (
    <table className="w-full text-left text-sm">
      <thead>
        <tr className="border-b border-border/60">
          <th className="pb-2 font-medium text-muted-foreground">Person</th>
          <th className="pb-2 font-medium text-muted-foreground">Role</th>
          {showActions ? <th className="pb-2 sr-only">Actions</th> : null}
        </tr>
      </thead>
      <tbody>
        {members.map((member) => (
          <MemberRow
            key={member.userId}
            slug={slug}
            member={member}
            canManage={canManage}
            showActions={showActions}
            isViewer={
              // Guarded against undefined on both sides. Without the check a
              // deployment where user_id did not arrive would match the first
              // member with no id and offer to remove a stranger.
              Boolean(viewerUserId) && member.userId === viewerUserId
            }
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
  showActions,
  isViewer,
}: {
  slug: string
  member: Member
  canManage: boolean
  showActions: boolean
  isViewer: boolean
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
        {isViewer ? (
          <Badge variant="secondary" className="ml-2 align-middle">
            You
          </Badge>
        ) : null}
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

      {showActions ? (
        <td className="py-3 text-right">
          {isViewer ? (
            // Leaving is confirmed, removing somebody else is not, and the
            // asymmetry is deliberate. Removing a colleague is reversible by
            // inviting them back. Removing yourself takes your own access away
            // immediately, and if you were the last owner able to invite, there
            // may be no way back without an operator opening a database
            // session. That asymmetry deserves a stop, and nothing else here
            // does.
            <Dialog>
              <DialogTrigger
                render={<Button type="button" variant="outline" size="sm" />}
              >
                Leave
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Leave this organisation?</DialogTitle>
                  <DialogDescription>
                    You will lose access to its compliance records, findings and
                    settings straight away. Another owner has to invite you back
                    to undo it.
                  </DialogDescription>
                </DialogHeader>
                <DialogFooter>
                  <DialogClose
                    render={<Button type="button" variant="outline" />}
                  >
                    Stay
                  </DialogClose>
                  <form action={remove}>
                    <input type="hidden" name="userId" value={member.userId} />
                    <Button type="submit" variant="destructive">
                      Leave organisation
                    </Button>
                  </form>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          ) : canManage ? (
            <form action={remove}>
              <input type="hidden" name="userId" value={member.userId} />
              <Button type="submit" variant="ghost" size="sm">
                Remove
              </Button>
            </form>
          ) : null}
        </td>
      ) : null}
    </tr>
  )
}
