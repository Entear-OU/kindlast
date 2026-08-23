import Link from 'next/link'
import { MailX } from 'lucide-react'

import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

/**
 * What somebody sees when an invitation link did not work (ENT-267).
 *
 * # WHY IT SAYS SO LITTLE
 *
 * core-api answers four quite different situations identically: the token
 * expired, it was already redeemed, it never existed, and it names somebody
 * other than the person holding the session. That is deliberate, and it is a
 * security property rather than terseness. Anything that told the four apart
 * would let anyone holding a session probe which invitations exist, and this
 * page would be the thing serving the answer.
 *
 * So the message states the one fact that is true in all four cases and is not
 * an oracle: it did not work, with this account. `__tests__/components/console/
 * invitation-failed.test.tsx` asserts the absence of the giveaway words rather
 * than trusting the wording to stay careful.
 *
 * # WHY IT NAMES THE ACCOUNT
 *
 * Because that is the difference between a person who can act and a person who
 * gives up. Since PR #227 the commonest reason to be here is being signed in
 * as the wrong person: an invitation is refused for anybody except the address
 * it names, which is exactly what happens when an inviter opens their own link
 * to see what the recipient will see. Naming the signed-in account tells them
 * what to change without telling them what the invitation said.
 *
 * # WHY THERE IS A WAY ONWARD
 *
 * The old behaviour dropped these people into their own organisation with no
 * message. Replacing that with a message and no exit would trade one dead end
 * for another, so the organisation they do belong to is one click away. It is
 * omitted rather than invented when there is none: a caller with no membership
 * is reachable, and a fabricated `/o/undefined` would 404 and read as data
 * loss.
 */
export function InvitationFailed({
  email,
  continueTo,
}: {
  /** The signed-in account, when it could be read. */
  email?: string
  /** Where they can go instead, or nothing when they belong nowhere yet. */
  continueTo?: string | null
}) {
  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <Empty data-testid="invitation-failed" className="border border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <MailX aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>That invitation could not be used</EmptyTitle>
          <EmptyDescription>
            {email
              ? `It could not be used with the account you are signed in with, ${email}.`
              : 'It could not be used with the account you are signed in with.'}{' '}
            If you hold another account, sign out and open the link again with
            that one. Otherwise ask whoever invited you to send a fresh link.
          </EmptyDescription>
        </EmptyHeader>
        {continueTo ? (
          <EmptyContent>
            <Link
              href={continueTo}
              className="text-sm font-medium text-foreground underline underline-offset-4"
            >
              Continue to your workspace
            </Link>
          </EmptyContent>
        ) : null}
      </Empty>
    </main>
  )
}
