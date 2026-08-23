import { redirect } from 'next/navigation'

import { currentSession } from '@/lib/auth/session'
import { InvitationFailed } from '@/components/console/invitation-failed'
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
 *
 * # THE ONE THING IT WILL NOT RESOLVE PAST (ENT-267)
 *
 * `?error=invitation`. Both halves of the invitation flow send a failed
 * redemption here, because this is the one authenticated path that works
 * without knowing a slug, and until now the parameter was set by two routes
 * and read by none. Resolving onward is precisely the wrong thing to do with
 * it: the person arrives in an organisation of their own, having been told
 * nothing, and reasonably concludes the link is broken.
 *
 * So this stops instead, says the invitation did not work, and offers the
 * organisation it would have redirected to as a link. The explanation is not
 * carried into `/o/{slug}` as a parameter, deliberately: that path goes
 * through the compliance-profile gate, which redirects to onboarding for
 * anybody who has not finished it, and a message that survives one arrival and
 * not another is worse than one that always appears in the same place.
 */
export default async function WorkspacePage({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>
}) {
  const session = await currentSession()

  // The proxy checks only that a cookie is present, which is not
  // authorization. This is the check that means anything.
  if (!session) redirect('/sign-in?returnTo=%2Fworkspace')

  // Repeated keys give an array. Reading the first is not a decision worth
  // making twice, and only one value is acted on.
  const error = (await searchParams)?.error
  const invitationFailed =
    (Array.isArray(error) ? error[0] : error) === 'invitation'

  const me = await loadCurrentUser(session.accessToken)
  const landing = landingPathFor(me)

  if (invitationFailed) {
    return <InvitationFailed email={me?.user?.email} continueTo={landing} />
  }

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
