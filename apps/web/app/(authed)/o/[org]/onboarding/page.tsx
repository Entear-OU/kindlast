import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { Interview } from '@/components/onboarding/interview'
import { StartButton } from '@/components/onboarding/start-button'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { getOnboardingSession } from '@/lib/onboarding/client'
import {
  answerQuestionAction,
  confirmProfileAction,
  startOnboardingAction,
} from './actions'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Getting set up',
}

/**
 * Onboarding (ENT-212, §24 step 6).
 *
 * # WHY THIS PAGE IS OUTSIDE THE PROFILE GATE
 *
 * Every other console route sits under `(needs-profile)/`, whose layout routes
 * a member of an unprofiled organisation here. This one deliberately does not:
 * a gate that redirected to itself would be an infinite loop, and the person
 * this exists for is precisely the one the gate keeps catching.
 *
 * # WHAT IT IS FOR
 *
 * ENT-166 recorded the defect this closes. A member whose organisation has no
 * compliance profile could reach the record registers, fill in an add form,
 * submit, and only then be told "this organisation has no compliance profile
 * yet: finish onboarding first". That sentence was correct and instructed the
 * reader to do something the product offered no way to do. This is the way.
 *
 * # NOTHING IS BELIEVED UNTIL IT IS CONFIRMED
 *
 * Answers live in the transcript until the person confirms them. Until then no
 * fact exists, no profile row exists, and no finding can have been reasoned
 * from any of it. That is structural rather than a screen somebody could skip.
 *
 * # AND NOTHING HERE TELLS ANYBODY WHAT THE LAW REQUIRES (ENT-248)
 *
 * There is no narrative on this page: no model writes prose here, and no copy
 * on it states that an obligation applies or does not. The obvious next feature
 * is a summary of what the answers mean, and it must not be built here as a
 * free-text renderer. ENT-248's ruling is that the statement of law comes
 * VERBATIM from the corpus row and the model only personalises around it,
 * because the narrator was observed citing Article 30 correctly while stating
 * the law wrongly beside it, and a citation validator cannot catch that. When
 * that lands, this page consumes it rather than growing a second path.
 */
export default async function OnboardingPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/onboarding'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Getting set up" />

  const result = await getOnboardingSession(
    session.accessToken,
    resolved.membership.orgId,
  )
  if (!result.ok) return <WorkspaceUnavailable title="Getting set up" />

  const state = result.value.state ?? {}
  const started = Boolean(state.sessionId)
  const finished = state.status === 'completed'

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {finished ? 'You are set up' : 'Getting set up'}
      </h1>

      {finished ? (
        <>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Everything you told us is recorded, and the Watcher now has
            something to check your circumstances against. Nothing here is
            fixed: correcting a fact keeps the previous answer, so you can see
            what we believed when an older finding was produced.
          </p>
          <div className="mt-6 flex flex-wrap gap-4 text-sm">
            <Link className="underline underline-offset-4" href={orgPath(slug)}>
              Go to the dashboard
            </Link>
            <Link
              className="underline underline-offset-4"
              href={orgPath(slug, '/settings/memory')}
            >
              See and correct what Kindlast knows
            </Link>
          </div>
        </>
      ) : (
        <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
          A short interview, about eleven questions, so Kindlast knows what your
          organisation actually does. Nothing is recorded until you have read it
          back and said it is right, and you can stop and come back: your
          answers are kept as you go.
        </p>
      )}

      <div className="mt-8">
        {finished ? null : started ? (
          <Interview
            slug={slug}
            state={state}
            answer={answerQuestionAction}
            confirm={confirmProfileAction}
          />
        ) : (
          <StartButton slug={slug} start={startOnboardingAction} />
        )}
      </div>
    </div>
  )
}
