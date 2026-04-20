import { NextRequest, NextResponse } from 'next/server'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { assessGDPRCompliance } from '@/lib/ai/assess-gdpr'
import type { BusinessProfile } from '@/lib/types/database'

export async function POST(request: NextRequest) {
  try {
    const config = getApiConfig()
    const cookieStore = await cookies()
    const accessToken = cookieStore.get(config.accessTokenCookie)?.value

    if (!accessToken) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    const body = await request.json()
    const { profileId } = body

    // Fetch business profile from Gateway
    const profileUrl = buildApiUrl(API_ENDPOINTS.profile, config)
    const profileResponse = await fetch(profileUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    if (!profileResponse.ok) {
      return NextResponse.json({ error: 'Profile not found' }, { status: 404 })
    }

    const profile: BusinessProfile = await profileResponse.json()

    // Verify the profile matches the requested profileId if provided
    if (profileId && profile.id !== profileId) {
      return NextResponse.json({ error: 'Profile mismatch' }, { status: 403 })
    }

    // Create assessment via Gateway
    const assessmentUrl = buildApiUrl(API_ENDPOINTS.assessments.create, config)
    const assessmentResponse = await fetch(assessmentUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${accessToken}`,
      },
      body: JSON.stringify({ type: 'gdpr' }),
    })

    if (!assessmentResponse.ok) {
      return NextResponse.json({ error: 'Failed to create assessment' }, { status: 500 })
    }

    const assessment = await assessmentResponse.json()

    // Run AI assessment with the user's access token
    const result = await assessGDPRCompliance(profile, accessToken)

    // Update assessment with results via Gateway
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

    // Note: Findings are embedded in the result JSON
    // A future enhancement would be to store findings separately via Gateway

    return NextResponse.json({ assessmentId: assessment.id })
  } catch (error) {
    console.error('Assessment error:', error)
    return NextResponse.json({ error: 'Internal server error' }, { status: 500 })
  }
}
