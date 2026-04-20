'use server'

import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { createCheckoutSession, createCustomerPortalSession } from '@/lib/stripe'

interface GatewayUser {
  id: string
  email: string
  plan: string
}

interface UserPlan {
  plan: string
  stripe_customer_id?: string
}

async function getAuthenticatedUser(): Promise<GatewayUser> {
  const config = getApiConfig()
  const cookieStore = await cookies()
  const accessToken = cookieStore.get(config.accessTokenCookie)?.value

  if (!accessToken) {
    throw new Error('Unauthorized')
  }

  const url = buildApiUrl(API_ENDPOINTS.auth.me, config)
  const response = await fetch(url, {
    headers: { 'Authorization': `Bearer ${accessToken}` },
  })

  if (!response.ok) {
    throw new Error('Unauthorized')
  }

  return response.json()
}

export async function createCheckout() {
  const user = await getAuthenticatedUser()

  const session = await createCheckoutSession(user.id, user.email)

  return { url: session.url }
}

export async function createPortalSession() {
  const user = await getAuthenticatedUser()

  const config = getApiConfig()
  const cookieStore = await cookies()
  const accessToken = cookieStore.get(config.accessTokenCookie)?.value

  // Fetch user plan info from Gateway
  const planUrl = buildApiUrl(API_ENDPOINTS.users.plan, config)
  const planResponse = await fetch(planUrl, {
    headers: { 'Authorization': `Bearer ${accessToken}` },
  })

  if (!planResponse.ok) {
    throw new Error('Failed to fetch subscription')
  }

  const plan: UserPlan = await planResponse.json()

  if (!plan.stripe_customer_id) {
    throw new Error('No subscription found')
  }

  const session = await createCustomerPortalSession(plan.stripe_customer_id)

  return { url: session.url }
}
