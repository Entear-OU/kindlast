'use server'

import { createClient } from '@/lib/supabase/server'
import { createCheckoutSession, createCustomerPortalSession } from '@/lib/stripe'

export async function createCheckout() {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) throw new Error('Unauthorized')

  const session = await createCheckoutSession(user.id, user.email!)

  return { url: session.url }
}

export async function createPortalSession() {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) throw new Error('Unauthorized')

  const { data: subscription } = await supabase
    .from('subscriptions')
    .select('stripe_customer_id')
    .eq('user_id', user.id)
    .maybeSingle()

  if (!subscription?.stripe_customer_id) {
    throw new Error('No subscription found')
  }

  const session = await createCustomerPortalSession(subscription.stripe_customer_id)

  return { url: session.url }
}
