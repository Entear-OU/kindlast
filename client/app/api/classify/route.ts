import { NextResponse } from 'next/server'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { classifyAIRisk } from '@/lib/ai/classify-ai-risk'
import type { BusinessProfile } from '@/lib/types/database'

export async function POST() {
  try {
    const config = getApiConfig()
    const cookieStore = await cookies()
    const accessToken = cookieStore.get(config.accessTokenCookie)?.value

    if (!accessToken) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    // Check user plan for premium access
    const planUrl = buildApiUrl(API_ENDPOINTS.users.plan, config)
    const planResponse = await fetch(planUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    if (!planResponse.ok) {
      return NextResponse.json({ error: 'Failed to check subscription' }, { status: 500 })
    }

    const plan = await planResponse.json()
    if (plan.plan !== 'premium' && plan.plan !== 'professional' && plan.plan !== 'team') {
      return NextResponse.json(
        { error: 'Premium subscription required' },
        { status: 403 }
      )
    }

    // Fetch profile from Gateway
    const profileUrl = buildApiUrl(API_ENDPOINTS.profile, config)
    const profileResponse = await fetch(profileUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    if (!profileResponse.ok) {
      return NextResponse.json({ error: 'Profile not found' }, { status: 404 })
    }

    const profile: BusinessProfile = await profileResponse.json()

    if (!profile.ai_system_descriptions) {
      return NextResponse.json(
        { error: 'No AI systems found in your profile' },
        { status: 400 }
      )
    }

    const aiSystems = profile.ai_system_descriptions as Array<{
      name: string
      purpose: string
      dataUsed: string
      isAutomatedDecision: boolean
    }>

    const result = await classifyAIRisk(aiSystems)

    // Create ai_act assessment via Gateway
    const assessmentUrl = buildApiUrl(API_ENDPOINTS.assessments.create, config)
    const assessmentResponse = await fetch(assessmentUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${accessToken}`,
      },
      body: JSON.stringify({ type: 'ai_act' }),
    })

    if (assessmentResponse.ok) {
      const assessment = await assessmentResponse.json()

      // Update with results
      const updateUrl = buildApiUrl(API_ENDPOINTS.assessments.update(assessment.id), config)
      await fetch(updateUrl, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${accessToken}`,
        },
        body: JSON.stringify({
          status: 'complete',
          result: result,
        }),
      })
    }

    return NextResponse.json(result)
  } catch (error) {
    console.error('AI Act classification error:', error)
    return NextResponse.json(
      { error: 'Classification failed' },
      { status: 500 }
    )
  }
}
