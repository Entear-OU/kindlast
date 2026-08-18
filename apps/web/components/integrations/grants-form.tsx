'use client'

import { useActionState } from 'react'

import { idle, type ActionState } from '@/lib/org/action-state'
import { toolsByRisk, type Integration } from '@/lib/integrations/client'

/**
 * Changing what a connection may do, and switching it off (ENT-231).
 *
 * # THE ALLOW-LIST IS PER CONNECTION, WHICH IS WHY THIS IS ONE FORM PER
 * # CONNECTION
 *
 * A single screen listing every tool across every connection would be shorter
 * and would lose the thing that matters: `search` on a helpdesk and `search`
 * on a document store are different permissions over different data, and a
 * combined list invites somebody to think of them as one setting.
 *
 * # REVOKING IS A SEPARATE FORM, NOT A BUTTON INSIDE THIS ONE
 *
 * Two submit buttons in one form is how somebody revokes a connection while
 * meaning to save a grant. They post to different actions, and the revoke one
 * says what it does rather than saying "remove".
 */
export function GrantsForm({
  slug,
  integration,
  update,
  revoke,
}: {
  slug: string
  integration: Integration
  update: (
    slug: string,
    previous: ActionState,
    form: FormData,
  ) => Promise<ActionState>
  revoke: (
    slug: string,
    previous: ActionState,
    form: FormData,
  ) => Promise<ActionState>
}) {
  const [grantState, grantAction, granting] = useActionState(
    update.bind(null, slug),
    idle,
  )
  const [revokeState, revokeActionFn, revoking] = useActionState(
    revoke.bind(null, slug),
    idle,
  )

  const tools = toolsByRisk(integration.tools ?? [])

  return (
    <div className="rounded-xl border border-border/60 bg-background p-4">
      <p className="text-sm font-medium text-foreground">
        {integration.displayName}
      </p>

      <form action={grantAction} className="mt-3 space-y-3">
        <input type="hidden" name="integrationId" value={integration.id} />

        <ul className="space-y-2">
          {tools.map((tool) => (
            <li key={tool.name} className="flex items-start gap-3">
              <input
                id={`${integration.id}-${tool.name}`}
                type="checkbox"
                name="grantedTools"
                value={tool.name}
                defaultChecked={Boolean(tool.granted)}
                className="mt-1"
              />
              <label
                htmlFor={`${integration.id}-${tool.name}`}
                className="text-xs text-foreground"
              >
                <span className="font-mono">{tool.name}</span>
                <span className="ml-2 text-muted-foreground">
                  {tool.writeCapable
                    ? 'can change data in that system'
                    : 'read only'}
                </span>
              </label>
            </li>
          ))}
        </ul>

        <button
          type="submit"
          disabled={granting}
          className="rounded-lg border border-border/60 px-3 py-2 text-xs font-medium text-foreground disabled:opacity-60"
        >
          Record what Kindlast may call
        </button>

        {grantState.status !== 'idle' && grantState.message ? (
          <p
            role="status"
            className={
              grantState.status === 'error'
                ? 'text-xs text-destructive'
                : 'text-xs text-muted-foreground'
            }
          >
            {grantState.message}
          </p>
        ) : null}
      </form>

      <form
        action={revokeActionFn}
        className="mt-4 border-t border-border/60 pt-3"
      >
        <input type="hidden" name="integrationId" value={integration.id} />
        <button
          type="submit"
          disabled={revoking}
          className="text-xs font-medium text-destructive disabled:opacity-60"
        >
          Revoke this connection permanently
        </button>
        <p className="mt-1 text-xs text-muted-foreground">
          Kindlast will fetch nothing further from it. What it already read
          stays in your record, and reconnecting is a new connection with a new
          agreement.
        </p>

        {revokeState.status !== 'idle' && revokeState.message ? (
          <p
            role="status"
            className={
              revokeState.status === 'error'
                ? 'mt-1 text-xs text-destructive'
                : 'mt-1 text-xs text-muted-foreground'
            }
          >
            {revokeState.message}
          </p>
        ) : null}
      </form>
    </div>
  )
}
