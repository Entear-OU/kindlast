import { createClient } from '@/lib/supabase/server'
import { redirect } from 'next/navigation'
import Link from 'next/link'
import { getBusinessProfile, getLatestAssessment, getFindings } from '@/lib/supabase/queries'
import { ScoreCard } from '@/components/dashboard/score-card'
import { FindingsSummary } from '@/components/dashboard/findings-summary'
import { RecentFindings } from '@/components/dashboard/recent-findings'
import { AssessmentPolling } from '@/components/dashboard/assessment-polling'
import { LegalDisclaimer } from '@/components/dashboard/legal-disclaimer'
import { RunAssessmentButton } from '@/components/dashboard/run-assessment-button'
import type { Assessment, Finding, BusinessProfile } from '@/lib/types/database'

export default async function DashboardPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()

  if (!user) {
    redirect('/login')
  }

  // Check for business profile — redirect to onboarding if missing
  const { data: profile } = await getBusinessProfile(supabase, user.id)
  if (!profile) {
    redirect('/dashboard/onboarding')
  }

  // Fetch latest assessment
  const { data: assessment } = await getLatestAssessment(supabase, user.id)
  const typedAssessment = assessment as Assessment | null

  // Fetch findings if assessment exists and is complete
  let findings: Finding[] = []
  if (typedAssessment?.id && typedAssessment.status === 'complete') {
    const { data: findingsData } = await getFindings(supabase, typedAssessment.id)
    findings = (findingsData as Finding[]) || []
  }

  const typedProfile = profile as BusinessProfile

  // Show processing state if assessment is pending or processing
  if (
    typedAssessment &&
    (typedAssessment.status === 'pending' || typedAssessment.status === 'processing')
  ) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <AssessmentPolling
          status={typedAssessment.status as 'pending' | 'processing'}
          profileId={typedProfile.id}
        />
        <LegalDisclaimer />
      </div>
    )
  }

  // No assessment yet
  if (!typedAssessment) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="rounded-lg border bg-card p-8 text-center">
          <h2 className="text-lg font-semibold">No Assessment Yet</h2>
          <p className="mt-2 mb-4 text-sm text-muted-foreground">
            Run your first GDPR compliance assessment to see your score and findings.
          </p>
          <RunAssessmentButton profileId={typedProfile.id} />
        </div>
        <LegalDisclaimer />
      </div>
    )
  }

  // Assessment complete (or error) — show results
  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <Link
          href="/dashboard/findings"
          className="inline-flex items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow hover:bg-primary/90 transition-colors"
        >
          Re-run Assessment
        </Link>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        <ScoreCard
          score={typedAssessment.overall_score ?? 0}
          riskLevel={typedAssessment.risk_level ?? 'unknown'}
        />
        <FindingsSummary findings={findings} />
      </div>

      <RecentFindings findings={findings} />

      <LegalDisclaimer />
    </div>
  )
}
