import { TriangleAlert } from 'lucide-react'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

/**
 * What an authenticated page shows when it cannot read the caller's own
 * memberships.
 *
 * WHY THIS EXISTS RATHER THAN `return null`
 *
 * Every page under `(authed)/o/[org]` used to render nothing in this case, which
 * puts the console shell around an empty column: no content, no explanation,
 * nothing to click. Met in a browser after a session's access token expired,
 * which is the commonest way to reach this branch and looks exactly like the
 * product being broken.
 *
 * NOT A REDIRECT TO SIGN-IN, AND THAT IS THE DECISION WORTH READING
 *
 * `resolveOrg` reports `unavailable` when `GetCurrentUser` fails, and it cannot
 * tell an expired token from core-api being down: both are "the call did not
 * answer". Redirecting on that would bounce somebody between the console and the
 * sign-in page every time the API hiccupped, while their session was perfectly
 * fine, and a redirect loop is a worse failure than a sentence.
 *
 * So it names both possibilities and lets the person judge. Saying "your session
 * expired" outright would be a guess presented as a fact, which is the habit this
 * product exists to avoid.
 *
 * The heading is passed in so each surface keeps its own, rather than every page
 * suddenly calling itself something generic at the moment it fails.
 */
export function WorkspaceUnavailable({ title }: { title: string }) {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {title}
      </h1>
      <Empty
        data-testid="workspace-unavailable"
        className="mt-4 border border-dashed"
      >
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlert aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>We could not load your workspace</EmptyTitle>
          <EmptyDescription>
            Your session may have expired, in which case signing in again will
            fix it. Otherwise this is usually temporary and worth reloading.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    </div>
  )
}
