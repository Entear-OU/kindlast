import { redirect } from 'next/navigation'

import { currentSession } from '@/lib/auth/session'
import { landingPathFor, loadCurrentUser } from '@/lib/auth/org'

/**
 * The old authenticated entry point, now a resolver (ENT-198).
 *
 * `/workspace` was where ENT-197 put the first page that proved the new stack
 * worked end to end. Every console route now lives under `/o/{slug}/`, so this
 * path redirects rather than 404s: it is in bookmarks and in `DEFAULT_RETURN_TO`,
 * and both have to keep working.
 *
 * It stays useful beyond compatibility. "Take me to my workspace" is a real
 * intent that does not know a slug yet, which is exactly what a sign-in
 * returning with no destination has, so this is the one place that turns
 * "somewhere" into "here".
 *
 * Someone with no organisation at all is not redirected in a circle. That
 * state is reachable: provisioning is idempotent but it can fail, and a person
 * holding a valid session with no membership must land on something that
 * explains itself rather than on a loop between two paths.
 */
export default async function WorkspacePage() {
  const session = await currentSession()

  // The proxy checks only that a cookie is present, which is not
  // authorization. This is the check that means anything.
  if (!session) redirect('/sign-in?returnTo=%2Fworkspace')

  const me = await loadCurrentUser(session.accessToken)
  const landing = landingPathFor(me)

  if (landing) redirect(landing)

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Workspace
      </h1>
      {me ? (
        <p data-testid="no-org" className="mt-2 text-sm text-muted-foreground">
          You do not belong to an organisation yet, so there is nothing to open.
          Signing out and back in will create one.
        </p>
      ) : (
        <p
          data-testid="workspace-unavailable"
          className="mt-2 text-sm text-muted-foreground"
        >
          The workspace service could not be reached, so your organisations
          could not be listed. Your session is unaffected: reload in a moment.
        </p>
      )}
    </main>
  )
}
