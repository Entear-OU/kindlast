import type { Metadata } from 'next'
import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { Interview } from '@/components/onboarding/interview'
import { StartButton } from '@/components/onboarding/start-button'
import { OBLIGATIONS } from '@/lib/onboarding/corpus'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import { getOnboardingSession } from '@/lib/onboarding/client'
import { answerQuestionAction, startOnboardingAction } from './actions'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Getting set up',
}

/**
 * Onboarding, which is the readiness assessment (ENT-212, ENT-254).
 *
 * # WHY THIS PAGE IS OUTSIDE THE PROFILE GATE
 *
 * Every other console route sits under `(needs-profile)/`, whose layout routes
 * a member of an unprofiled organisation here. This one deliberately does not:
 * a gate that redirected to itself would be an infinite loop, and the person
 * this exists for is precisely the one the gate keeps catching.
 *
 * The gate reads `profile_exists`, and onboarding writes the compliance profile
 * on the last answer rather than on the first, so somebody who stops half way
 * through is routed back here and picks up where they left off. That is what
 * "completes it in one pass" is made of.
 *
 * # WHAT IT IS FOR
 *
 * ENT-166 recorded the defect this closes. A member whose organisation has no
 * compliance profile could reach the record registers, fill in an add form,
 * submit, and only then be told "this organisation has no compliance profile
 * yet: finish onboarding first". That sentence was correct and instructed the
 * reader to do something the product offered no way to do. This is the way.
 *
 * # EVERY ANSWER IS SAVED AS IT IS GIVEN
 *
 * ENT-254 removed the confirmation step. The property it protected, that
 * nothing is believed which the person has not seen, is kept where it is
 * cheaper: the parsed value sits beside the answer at the moment it is given,
 * and every fact is correctable afterwards with its history intact.
 *
 * # AND EVERY STATEMENT OF LAW ON THIS PAGE IS A CORPUS ROW (ENT-248)
 *
 * No model writes prose here and no copy on this page states that an obligation
 * applies or does not. What the law says is quoted from `data/corpus/`,
 * unedited, with the citation beside it; what is said about the organisation is
 * written from its own answers and asserts nothing legal. The narrator was
 * observed citing Article 30 correctly and stating the law wrongly beside it,
 * and a citation validator cannot catch that, which is why the split is a
 * rendering rule here rather than only a prompt rule elsewhere.
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
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        {finished ? 'Where you stand' : 'Getting set up'}
      </h1>

      {finished ? null : (
        <div className="mt-2 max-w-2xl space-y-1">
          <p className="text-sm text-muted-foreground">
            Eleven questions, all of them a tap, so Kindlast knows what your
            organisation actually does. The column beside them is the regulation
            Kindlast holds, narrowing as you answer.
          </p>
          {/* The count comes from the corpus rather than from a copywriter, so
              adding a regulation pack moves the number here instead of leaving
              it quietly wrong. */}
          <p className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            {`${OBLIGATIONS.length} obligations · GDPR and EU AI Act · About two minutes`}
          </p>
        </div>
      )}

      <div className="mt-8">
        {started ? (
          <Interview
            slug={slug}
            state={state}
            answer={answerQuestionAction}
            dashboardHref={orgPath(slug)}
            memoryHref={orgPath(slug, '/settings/memory')}
          />
        ) : (
          <StartButton slug={slug} start={startOnboardingAction} />
        )}
      </div>
    </div>
  )
}
