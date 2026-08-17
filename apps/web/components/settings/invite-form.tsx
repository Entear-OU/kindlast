'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { inviteMemberAction } from '@/app/(authed)/o/[org]/settings/actions'
import { idle, type ActionState } from '@/lib/org/action-state'

const ROLES = ['owner', 'member', 'viewer'] as const

/**
 * Invite somebody to the organisation (ENT-219).
 *
 * This replaces the "not available yet" note ENT-202 shipped in its place. That
 * note was correct at the time and the reasoning is worth keeping: a control
 * that creates an invitation nobody can be told about is worse than one that is
 * honestly missing, because the failure is invisible from both ends. The owner
 * sees a success, the invitee never hears anything, and the raw token is gone
 * so nothing can recover it.
 *
 * What changed is not this component's caution but the thing underneath it.
 * core-api now writes the email onto the transactional outbox in the same
 * transaction as the invitation, so a successful call means a real message
 * exists and is queued.
 *
 * The role defaults to `member` rather than to the first item in the list.
 * `owner` is first because it reads as a hierarchy, and a form that defaults to
 * the most powerful role is one where a distracted person grants ownership by
 * pressing enter.
 */
export function InviteForm({ slug }: { slug: string }) {
  const [state, invite] = useActionState<ActionState, FormData>(
    inviteMemberAction.bind(null, slug),
    idle,
  )

  return (
    <form action={invite} className="flex flex-wrap items-end gap-2">
      <div className="grow">
        <Label htmlFor="invite-email" className="sr-only">
          Email address to invite
        </Label>
        <Input
          id="invite-email"
          name="email"
          type="email"
          autoComplete="off"
          placeholder="colleague@example.com"
        />
      </div>

      <div>
        <Label htmlFor="invite-role" className="sr-only">
          Role for the invited person
        </Label>
        <select
          id="invite-role"
          name="role"
          defaultValue="member"
          className="h-9 rounded-md border border-border/60 bg-background px-2 text-sm"
        >
          {ROLES.map((role) => (
            <option key={role} value={role}>
              {role}
            </option>
          ))}
        </select>
      </div>

      <Button type="submit" variant="outline" size="sm">
        Send invitation
      </Button>

      {state.status !== 'idle' ? (
        <p
          // `status` rather than `alert` for the success case: an invitation
          // going out is not an interruption, and a screen reader announcing it
          // as one would talk over whatever the person does next.
          role={state.status === 'error' ? 'alert' : 'status'}
          className={
            state.status === 'error'
              ? 'w-full text-xs text-destructive'
              : 'w-full text-xs text-muted-foreground'
          }
        >
          {state.message}
        </p>
      ) : null}
    </form>
  )
}
