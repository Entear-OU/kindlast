import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { InviteForm } from '@/components/settings/invite-form'
import { MembersTable } from '@/components/settings/members-table'
import { OrganisationForm } from '@/components/settings/organisation-form'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { NotificationsForm } from '@/components/settings/notifications-form'
import { channelsFor, membersOf, notificationsFor } from './actions'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Settings',
}

/**
 * Organisation settings (ENT-202).
 *
 * Two sections, organisation and members, which is the shape the legacy UI had
 * at db0bf83 and is now the convention the rest of the rebuild follows. When
 * notifications arrive (ENT-209) they are a third section whose channel list
 * comes from the capabilities endpoint rather than a hard-coded array (§18.3),
 * so this is laid out to take another section without restructuring.
 *
 * Resolution repeats the layout's rather than receiving it, because a layout
 * cannot pass props to a page. It costs nothing: loadCurrentUser is
 * request-cached, so both get the same answer from one round trip.
 */
export default async function SettingsPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/settings'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Settings" />

  const { membership, me } = resolved
  const canManage = membership.role === 'owner'

  // Null means the call failed, which is not the same as an organisation with
  // no members. The distinction is shown rather than collapsed: an empty table
  // would tell an owner their colleagues had vanished.
  const members = await membersOf(session.accessToken, membership.orgId)

  // Fetched together, because they are rendered together and each is a
  // round trip the page would otherwise make in series.
  const [notifications, channels] = await Promise.all([
    notificationsFor(session.accessToken, membership.orgId),
    channelsFor(session.accessToken, membership.orgId),
  ])

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Settings
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        {membership.orgName || slug}
      </p>

      <section className="mt-10">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Organisation
        </h2>
        <div className="mt-4">
          <OrganisationForm
            slug={slug}
            name={membership.orgName ?? ''}
            canManage={canManage}
          />
        </div>
      </section>

      <section className="mt-12">
        <div className="flex items-baseline justify-between">
          <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Members
          </h2>
        </div>

        {canManage ? (
          <div className="mt-4">
            <InviteForm slug={slug} />
          </div>
        ) : null}

        <div className="mt-4">
          {members === null ? (
            <p className="text-sm text-muted-foreground">
              Could not load the member list. Reload to try again.
            </p>
          ) : (
            /* `me.user.userId`, not `me.user.id` (ENT-220). The first is
               core-api's derived key and is what `Member.userId` carries; the
               second is the IdP's subject claim, which matches no row here.
               Passing the wrong one is silent: nothing matches, no row is
               marked, and leaving quietly disappears again. */
            <MembersTable
              slug={slug}
              members={members}
              viewerRole={membership.role ?? ''}
              viewerUserId={me.user?.userId}
            />
          )}
        </div>
      </section>

      <section className="mt-12">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Model
        </h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Which model reads this organisation&rsquo;s compliance data, and where
          it runs. Sending it to a hosted provider makes that provider a
          sub-processor you are responsible for recording, so it is a page of
          its own rather than a switch here.
        </p>
        <p className="mt-4">
          <Link
            href={orgPath(slug, '/settings/model')}
            className="text-sm text-foreground underline underline-offset-4"
          >
            Where this organisation is processed
          </Link>
        </p>
      </section>

      <section className="mt-12">
        <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Notifications
        </h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Yours alone, and only for this organisation. Nobody else can see or
          change them, and if you belong to more than one organisation each has
          its own.
        </p>
        <div className="mt-4">
          {notifications === null ? (
            // Null means the call failed, which is not the same as somebody who
            // has never changed anything. Rendering the defaults here would tell
            // a person their settings are one thing while the database holds
            // another, and they would have no way to notice.
            <p className="text-sm text-muted-foreground">
              Could not load your notification settings. Reload to try again.
            </p>
          ) : (
            <NotificationsForm
              slug={slug}
              preferences={notifications}
              channels={channels ?? []}
            />
          )}
        </div>
      </section>
    </main>
  )
}
