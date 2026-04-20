import { NextResponse } from 'next/server'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'

export async function GET() {
  try {
    const config = getApiConfig()
    const cookieStore = await cookies()
    const accessToken = cookieStore.get(config.accessTokenCookie)?.value

    if (!accessToken) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    // Fetch business profile from Gateway
    let profile = null
    try {
      const profileUrl = buildApiUrl(API_ENDPOINTS.profile, config)
      const profileResponse = await fetch(profileUrl, {
        headers: { 'Authorization': `Bearer ${accessToken}` },
      })

      if (profileResponse.ok) {
        profile = await profileResponse.json()
      }
    } catch {
      // Profile not found is OK
    }

    // Fetch user plan from Gateway
    let subscription = null
    try {
      const planUrl = buildApiUrl(API_ENDPOINTS.users.plan, config)
      const planResponse = await fetch(planUrl, {
        headers: { 'Authorization': `Bearer ${accessToken}` },
      })

      if (planResponse.ok) {
        subscription = await planResponse.json()
      }
    } catch {
      // Plan not found is OK
    }

    return NextResponse.json({
      profile,
      subscription,
    })
  } catch (error) {
    console.error('Settings fetch error:', error)
    return NextResponse.json({ error: 'Internal server error' }, { status: 500 })
  }
}
