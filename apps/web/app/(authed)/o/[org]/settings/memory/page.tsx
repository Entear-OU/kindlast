import { notFound, redirect } from 'next/navigation'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { EvidenceList } from '@/components/memory/evidence-list'
import { ProfileFactList } from '@/components/memory/profile-fact-list'
import { listEvidence, listProfileFacts } from '@/lib/memory/client'
import { correctFactAction } from './actions'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * What Kindlast knows about us (ENT-228, §26.5).
 *
 * # THE PAGE THAT MAKES THE MEMORY THE CUSTOMER'S
 *
 * §26.5 puts organisation memory in Postgres under RLS rather than in an agent
 * framework's store so a customer can see it, correct it, export it and have it
 * erased. A schema alone does not deliver that; this page is the seeing.
 *
 * A GDPR product whose own memory of a customer sits outside that customer's
 * reach cannot answer a request about it. Rectification and erasure are not
 * features here, they are the thing being sold, and a store nobody could point
 * a subject access request at would be the one place in this system where the
 * product's claim is false.
 *
 * # TWO SECTIONS BECAUSE THEY ARE TWO DIFFERENT QUESTIONS
 *
 * What we BELIEVE is a small correctable set. What we OBSERVED is an unbounded
 * append-only log. "What do you think our lawful basis is" and "what did the
 * tool actually return in March" are different questions, and answering both
 * from one list answers the first one badly.
 *
 * # NOTHING HERE IS AN EDIT
 *
 * A correction closes the current value and records a new one; the previous
 * answer stays. So the copy says "record what is true now" rather than "edit",
 * because a form that reads as editing is describing something the product
 * deliberately cannot do.
 */
export default async function MemoryPage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(
      `/sign-in?returnTo=${encodeURIComponent(orgPath(slug, '/settings/memory'))}`,
    )

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="What Kindlast knows" />

  const { membership } = resolved

  // Both in parallel: neither depends on the other, and this is one render
  // rather than a sequence.
  const [factsResult, evidenceResult] = await Promise.all([
    listProfileFacts(session.accessToken, membership.orgId),
    listEvidence(session.accessToken, membership.orgId),
  ])

  const facts = factsResult.ok ? (factsResult.value.facts ?? []) : []
  const evidence = evidenceResult.ok
    ? (evidenceResult.value.evidence ?? [])
    : []

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-8">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        What Kindlast knows about you
      </h1>
      <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
        Everything below is yours. Each value shows where it came from, and
        correcting one keeps the previous answer so you can see what we thought
        when an older finding was produced.
      </p>

      <section className="mt-8" aria-labelledby="profile">
        <h2
          id="profile"
          className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
        >
          What we believe
        </h2>

        {facts.length === 0 ? (
          // A real state rather than an empty box. A workspace that has not
          // been through onboarding genuinely has no profile, and every sweep
          // it runs is reasoning from nothing, which is worth saying plainly.
          <p className="mt-3 text-sm text-muted-foreground">
            We have not recorded anything about your organisation yet. Findings
            will be generic until we have, because there is nothing to check
            your circumstances against.
          </p>
        ) : (
          <ProfileFactList
            facts={facts}
            slug={slug}
            correct={correctFactAction}
          />
        )}
      </section>

      <section className="mt-10" aria-labelledby="evidence">
        <h2
          id="evidence"
          className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground"
        >
          What we observed
        </h2>
        <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
          Readings taken from your connected tools and anything recorded by
          hand. These are never edited: when something changes, the newer
          reading supersedes the older one and both stay.
        </p>

        {evidence.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">
            Nothing yet. Observations arrive once a tool is connected.
          </p>
        ) : (
          <EvidenceList evidence={evidence} />
        )}
      </section>
    </div>
  )
}
