'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  idle,
  renameOrganisationAction,
  type ActionState,
} from '@/app/(authed)/o/[org]/settings/actions'

/**
 * Renaming an organisation (ENT-202).
 *
 * The slug is shown next to the field, read-only, and the copy says it does
 * not change. That is not decoration: someone renaming after an acquisition
 * reasonably expects the URL to follow, and finding out later that it did not
 * is worse than being told now. It does not follow because slugs live in
 * bookmarks and in emailed capability links, which are exactly the links a
 * compliance product has to keep working.
 */
export function OrganisationForm({
  slug,
  name,
  canManage,
}: {
  slug: string
  name: string
  canManage: boolean
}) {
  const [state, rename] = useActionState<ActionState, FormData>(
    renameOrganisationAction.bind(null, slug),
    idle,
  )

  return (
    <form action={rename} className="max-w-md space-y-4">
      <div className="space-y-2">
        <Label htmlFor="org-name">Name</Label>
        <Input
          id="org-name"
          name="name"
          defaultValue={name}
          disabled={!canManage}
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="org-slug">Address</Label>
        <Input id="org-slug" value={`/o/${slug}`} readOnly disabled />
        <p className="text-xs text-muted-foreground">
          This stays the same when you rename, so existing links and bookmarks
          keep working.
        </p>
      </div>

      {canManage ? (
        <Button type="submit">Save changes</Button>
      ) : (
        <p className="text-sm text-muted-foreground">
          Only an owner can change these.
        </p>
      )}

      {state.status !== 'idle' ? (
        <p
          role="status"
          className={
            state.status === 'error'
              ? 'text-sm text-destructive'
              : 'text-sm text-muted-foreground'
          }
        >
          {state.message}
        </p>
      ) : null}
    </form>
  )
}
