import { notFound, redirect } from 'next/navigation'

import { MembersTable } from '@/components/settings/members-table'
import { OrganisationForm } from '@/components/settings/organisation-form'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { membersOf } from './actions'

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
  if (resolved.status === 'unavailable') return null

  const { membership } = resolved
  const canManage = membership.role === 'owner'

  // Null means the call failed, which is not the same as an organisation with
  // no members. The distinction is shown rather than collapsed: an empty table
  // would tell an owner their colleagues had vanished.
  const members = await membersOf(session.accessToken, membership.orgId)

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
          {canManage ? <InviteUnavailable /> : null}
        </div>

        <div className="mt-4">
          {members === null ? (
            <p className="text-sm text-muted-foreground">
              Could not load the member list. Reload to try again.
            </p>
          ) : (
            /* No "which row is you" marker, and its absence is a finding
               rather than an omission. GetCurrentUser returns the IdP's
               subject claim; ListMembers returns the version-5 uuid derived
               from it by libs/chassis/subject. The derivation is one-way, so
               a client holding one cannot recognise the other, and web has no
               way to identify itself in this list.

               That costs more than a label. An owner leaving an organisation
               is a legitimate act, and without knowing which row is theirs the
               page can neither offer it as "leave" nor warn before a
               self-removal. ENT-220 fixes it in the contract rather than
               here, because a client-side join through user_identities would
               be the console answering a question the API should. */
            <MembersTable
              slug={slug}
              members={members}
              viewerRole={membership.role ?? ''}
            />
          )}
        </div>
      </section>
    </main>
  )
}

/**
 * The invite control, visibly unavailable rather than present and inert.
 *
 * core-api can create an invitation today. Nothing can deliver one: the raw
 * token exists for the length of the handler that mints it and is then held
 * only as a hash, and the transactional outbox that will carry it is ENT-219.
 *
 * So the button is not rendered as a working control. An owner who clicked it
 * would get a created invitation, no email, and no way to know that the person
 * they invited will never hear about it. A control that silently does nothing
 * is worse than one that is honestly missing, and worse still here because the
 * failure is invisible from both ends.
 */
function InviteUnavailable() {
  return (
    <p className="text-xs text-muted-foreground">
      Inviting people is not available yet: invitations cannot be delivered
      until email is wired up (ENT-219).
    </p>
  )
}
