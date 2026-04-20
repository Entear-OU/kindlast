'use server'

import { cookies } from 'next/headers'
import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import type { FullProfileData } from '@/lib/schemas/onboarding'
import { assessGDPRCompliance } from '@/lib/ai/assess-gdpr'
import type { BusinessProfile } from '@/lib/types/database'

async function getAccessToken(): Promise<string | null> {
  const config = getApiConfig()
  const cookieStore = await cookies()
  return cookieStore.get(config.accessTokenCookie)?.value || null
}

export async function saveBusinessProfile(data: FullProfileData) {
  const accessToken = await getAccessToken()

  if (!accessToken) {
    throw new Error('Unauthorized')
  }

  const config = getApiConfig()
  const url = buildApiUrl(API_ENDPOINTS.profile, config)

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${accessToken}`,
    },
    body: JSON.stringify(data),
  })

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}))
    throw new Error(errorData.message || 'Failed to save profile')
  }

  const profile = await response.json()

  revalidatePath('/dashboard')
  return profile
}

export async function completeOnboarding() {
  const accessToken = await getAccessToken()

  if (!accessToken) {
    throw new Error('Unauthorized')
  }

  const config = getApiConfig()

  // Fetch the saved profile
  const profileUrl = buildApiUrl(API_ENDPOINTS.profile, config)
  const profileResponse = await fetch(profileUrl, {
    headers: { 'Authorization': `Bearer ${accessToken}` },
  })

  if (!profileResponse.ok) {
    throw new Error('Profile not found')
  }

  const profile: BusinessProfile = await profileResponse.json()

  // Auto-trigger first GDPR assessment
  try {
    // Create assessment with processing status
    const assessmentUrl = buildApiUrl(API_ENDPOINTS.assessments.create, config)
    const assessmentResponse = await fetch(assessmentUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${accessToken}`,
      },
      body: JSON.stringify({ type: 'gdpr' }),
    })

    if (assessmentResponse.ok) {
      const assessment = await assessmentResponse.json()

      // Run AI assessment with user's access token
      const result = await assessGDPRCompliance(profile, accessToken)

      // Update assessment with results
      const updateUrl = buildApiUrl(API_ENDPOINTS.assessments.update(assessment.id), config)
      await fetch(updateUrl, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${accessToken}`,
        },
        body: JSON.stringify({
          status: 'complete',
          overall_score: result.overall_score,
          risk_level: result.risk_level,
          result: result,
        }),
      })

      // Note: Findings creation should be handled by the Gateway
      // when the assessment is updated to 'complete'
      // For now, findings are embedded in the result JSON
    }
  } catch {
    // Non-blocking — user can trigger manually from dashboard
  }

  revalidatePath('/dashboard')
  redirect('/dashboard')
}
