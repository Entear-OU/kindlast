import { redirect } from 'next/navigation'
import { cookies } from 'next/headers'
import Link from 'next/link'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { ScoreCard } from '@/components/dashboard/score-card'
import { FindingsSummary } from '@/components/dashboard/findings-summary'
import { RecentFindings } from '@/components/dashboard/recent-findings'
import { AssessmentPolling } from '@/components/dashboard/assessment-polling'
import { LegalDisclaimer } from '@/components/dashboard/legal-disclaimer'
import { RunAssessmentButton } from '@/components/dashboard/run-assessment-button'
import type { Assessment, Finding, BusinessProfile } from '@/lib/types/database'

async function fetchWithAuth<T>(endpoint: string, accessToken: string): Promise<T | null> {
  const config = getApiConfig()
  try {
    const url = buildApiUrl(endpoint, config)
    const response = await fetch(url, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
      cache: 'no-store',
    })

    if (!response.ok) {
      return null
    }

    return await response.json()
  } catch {
    return null
  }
}

export default async function DashboardPage() {
  const config = getApiConfig()
  const cookieStore = await cookies()
  const accessToken = cookieStore.get(config.accessTokenCookie)?.value

  if (!accessToken) {
    redirect('/login')
  }

  // Check for business profile — redirect to onboarding if missing
  const profile = await fetchWithAuth<BusinessProfile>(API_ENDPOINTS.profile, accessToken)
  if (!profile) {
    redirect('/dashboard/onboarding')
  }

  // Fetch latest assessment
  const assessment = await fetchWithAuth<Assessment>(API_ENDPOINTS.assessments.latest, accessToken)

  // Fetch findings if assessment exists and is complete
  let findings: Finding[] = []
  if (assessment?.id && assessment.status === 'complete') {
    const findingsResponse = await fetchWithAuth<{ findings: Finding[] }>(
      API_ENDPOINTS.assessments.findings(assessment.id),
      accessToken
    )
    findings = findingsResponse?.findings || []
  }

  // Show processing state if assessment is pending or processing
  if (
    assessment &&
    (assessment.status === 'pending' || assessment.status === 'processing')
  ) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <AssessmentPolling
          status={assessment.status as 'pending' | 'processing'}
          profileId={profile.id}
        />
        <LegalDisclaimer />
      </div>
    )
  }

  // No assessment yet
  if (!assessment) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="rounded-lg border bg-card p-8 text-center">
          <h2 className="text-lg font-semibold">No Assessment Yet</h2>
          <p className="mt-2 mb-4 text-sm text-muted-foreground">
            Run your first GDPR compliance assessment to see your score and findings.
          </p>
          <RunAssessmentButton profileId={profile.id} />
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
          score={assessment.overall_score ?? 0}
          riskLevel={assessment.risk_level ?? 'unknown'}
        />
        <FindingsSummary findings={findings} />
      </div>

      <RecentFindings findings={findings} />

      <LegalDisclaimer />
    </div>
  )
}
