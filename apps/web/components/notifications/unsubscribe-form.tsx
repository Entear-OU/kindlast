'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { unsubscribeAction } from '@/app/unsubscribe/[token]/actions'
import { idle, type ActionState } from '@/lib/org/action-state'

/**
 * The button that actually spends an unsubscribe token (ENT-209).
 *
 * A form rather than a link, so the request is a POST. The page it sits on
 * explains why at length: a GET would let a mail gateway's link scanner
 * unsubscribe somebody by the act of delivering the email.
 *
 * The action is imported rather than received as a prop. Passing a server
 * action down from a server component works, and importing it keeps the
 * component's dependencies visible in its own header instead of arriving from
 * whichever page happens to render it.
 */
export function UnsubscribeForm({ token }: { token: string }) {
  const [state, unsubscribe] = useActionState<ActionState, FormData>(
    unsubscribeAction,
    idle,
  )

  // Once it has worked, the button goes. Leaving a spent control on screen
  // invites a second click, and the honest answer to that click is "that link
  // has already been used", which reads like a failure rather than like the
  // thing having worked the first time.
  if (state.status === 'ok') {
    return (
      <p role="status" className="text-sm text-foreground">
        {state.message}
      </p>
    )
  }

  return (
    <form action={unsubscribe} className="space-y-3">
      <input type="hidden" name="token" value={token} />
      <Button type="submit" variant="destructive">
        Stop sending me these
      </Button>
      {state.status === 'error' ? (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      ) : null}
    </form>
  )
}
