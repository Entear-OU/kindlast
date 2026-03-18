import Stripe from 'stripe'

let _stripe: Stripe | null = null

export function getStripe(): Stripe {
  if (!_stripe) {
    _stripe = new Stripe(process.env.STRIPE_SECRET_KEY!, {
      apiVersion: '2026-02-25.clover',
      typescript: true,
    })
  }
  return _stripe
}

export async function createCheckoutSession(userId: string, customerEmail: string) {
  const stripe = getStripe()
  const session = await stripe.checkout.sessions.create({
    customer_email: customerEmail,
    mode: 'subscription',
    line_items: [
      {
        price: process.env.STRIPE_PRICE_ID_PREMIUM!,
        quantity: 1,
      },
    ],
    success_url: `${process.env.NEXT_PUBLIC_APP_URL}/dashboard?upgraded=true`,
    cancel_url: `${process.env.NEXT_PUBLIC_APP_URL}/pricing`,
    metadata: {
      user_id: userId,
    },
  })

  return session
}

export async function createCustomerPortalSession(customerId: string) {
  const stripe = getStripe()
  const session = await stripe.billingPortal.sessions.create({
    customer: customerId,
    return_url: `${process.env.NEXT_PUBLIC_APP_URL}/dashboard/settings`,
  })

  return session
}
