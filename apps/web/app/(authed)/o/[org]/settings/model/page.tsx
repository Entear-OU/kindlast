import type { Metadata } from 'next'
import { notFound, redirect } from 'next/navigation'

import { ModelForm } from '@/components/settings/model-form'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { modelSettingFor } from './actions'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Model',
}

/**
 * Where this organisation's model runs (ENT-236, §26.6).
 *
 * # A PAGE OF ITS OWN, RATHER THAN A SECTION UNDER SETTINGS
 *
 * The other settings sections are preferences: a name, a member list, which
 * emails somebody wants. This one is a processing decision that lengthens the
 * customer's sub-processor list, and putting it between "Organisation" and
 * "Members" would say it belongs in the same category as renaming a workspace.
 *
 * It is readable by every member and writable only by an owner, and those are
 * different questions on purpose. "Where does our compliance data get
 * processed" is a thing anybody in the organisation may reasonably ask, and a
 * product answering it only for the person who can change it is one nobody else
 * can check.
 */
export default async function ModelSettingsPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/settings/model'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Model" />

  const { membership } = resolved
  const view = await modelSettingFor(session.accessToken, membership.orgId)

  return (
    <main className="mx-auto w-full max-w-3xl px-4 py-12">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Model
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Which model reads this organisation&rsquo;s compliance data, and where
        it runs.
      </p>

      <section className="mt-10">
        {view === null ? (
          /* Null means the call failed, which is not the same as an
             organisation using the bundled model. Rendering the safe default
             here would tell somebody nothing leaves this deployment on the
             strength of a request nobody completed, which is the one wrong
             answer on this page that nobody would think to question. */
          <p className="text-sm text-muted-foreground">
            Could not load where this organisation is processed. Reload to try
            again, and do not assume either answer until it loads.
          </p>
        ) : (
          <ModelForm
            slug={slug}
            view={view}
            canManage={membership.role === 'owner'}
          />
        )}
      </section>
    </main>
  )
}
