'use client'

import { useActionState } from 'react'
import Link from 'next/link'

import { Button } from '@/components/ui/button'
import { approveFromEmailAction } from '@/app/approve/[findingId]/[token]/actions'
import {
  idle,
  type ApprovalFromEmailState,
} from '@/lib/findings/approval-from-email-state'

/**
 * The button that actually spends an approve link (§8, ENT-249).
 *
 * A form rather than a link, so the request is a POST. The page it sits on
 * explains why at length, and it is the sharpest version of the argument in
 * this app: corporate mail gateways and link previewers follow every URL in a
 * message before a human sees it, so under a GET the act of delivering a
 * finding notification would approve the finding, in the customer's own
 * compliance record, naming somebody who never opened the message.
 *
 * Both halves of the credential travel in the body. The finding is in the URL
 * too, and core-api refuses a delegation whose binding does not match the
 * finding presented, so neither half is enough on its own.
 *
 * The action is imported rather than received as a prop, matching the
 * unsubscribe form: the component's dependencies stay visible in its own header
 * instead of arriving from whichever page happens to render it.
 */
export function ApproveFromEmailForm({
  findingId,
  token,
}: {
  findingId: string
  token: string
}) {
  const [state, approve] = useActionState<ApprovalFromEmailState, FormData>(
    approveFromEmailAction,
    idle,
  )

  // Once it has worked the button goes. Leaving a spent control on screen
  // invites a second click, and the honest answer to that click is "that link
  // has already been used", which reads like a failure rather than like the
  // thing having worked the first time.
  if (state.status === 'ok') {
    return (
      <div className="space-y-3">
        <p role="status" className="text-sm text-foreground">
          {state.message}
        </p>
        {state.destination ? (
          <Link
            href={state.destination}
            className="text-sm font-medium text-foreground underline underline-offset-4"
          >
            Open the finding in Kindlast
          </Link>
        ) : null}
      </div>
    )
  }

  return (
    <form action={approve} className="space-y-3">
      <input type="hidden" name="token" value={token} />
      <input type="hidden" name="findingId" value={findingId} />
      <Button type="submit">Approve this finding</Button>
      {state.status === 'error' ? (
        <p role="alert" className="text-sm text-destructive">
          {state.message}
        </p>
      ) : null}
    </form>
  )
}
