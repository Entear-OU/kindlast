'use server'

import { cookies } from 'next/headers'
import { revalidatePath } from 'next/cache'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'

async function getAccessToken(): Promise<string | null> {
  const config = getApiConfig()
  const cookieStore = await cookies()
  return cookieStore.get(config.accessTokenCookie)?.value || null
}

export async function toggleFindingResolved(findingId: string, resolved: boolean) {
  const accessToken = await getAccessToken()

  if (!accessToken) {
    throw new Error('Unauthorized')
  }

  const config = getApiConfig()
  const url = buildApiUrl(API_ENDPOINTS.findings.update(findingId), config)

  const response = await fetch(url, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${accessToken}`,
    },
    body: JSON.stringify({ is_resolved: resolved }),
  })

  if (!response.ok) {
    throw new Error('Failed to update finding')
  }

  revalidatePath('/dashboard/findings')
  revalidatePath('/dashboard')
}

export async function rerunAssessment(profileId: string) {
  const accessToken = await getAccessToken()

  if (!accessToken) {
    throw new Error('Unauthorized')
  }

  // Create a new assessment via the Gateway
  const config = getApiConfig()
  const url = buildApiUrl(API_ENDPOINTS.assessments.create, config)

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${accessToken}`,
    },
    body: JSON.stringify({ type: 'gdpr' }),
  })

  if (!response.ok) {
    throw new Error('Failed to start assessment')
  }

  const data = await response.json()

  revalidatePath('/dashboard')
  revalidatePath('/dashboard/findings')

  return data.id
}
