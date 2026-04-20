import { NextResponse } from 'next/server'
import Stripe from 'stripe'
import { getStripe } from '@/lib/stripe'

export async function POST(request: Request) {
  const body = await request.text()
  const signature = request.headers.get('stripe-signature')!

  let event: Stripe.Event

  try {
    event = getStripe().webhooks.constructEvent(
      body,
      signature,
      process.env.STRIPE_WEBHOOK_SECRET!
    )
  } catch {
    return NextResponse.json(
      { error: 'Invalid signature' },
      { status: 400 }
    )
  }

  // TODO: Implement Gateway-based subscription management
  // The Gateway should handle subscription status updates via its own database
  // For now, we log the event and acknowledge receipt

  switch (event.type) {
    case 'checkout.session.completed': {
      const session = event.data.object as Stripe.Checkout.Session
      console.log('Checkout completed:', {
        userId: session.metadata?.user_id,
        customerId: session.customer,
        subscriptionId: session.subscription,
      })
      // TODO: Call Gateway API to update user subscription
      break
    }

    case 'customer.subscription.updated': {
      const subscription = event.data.object as Stripe.Subscription
      console.log('Subscription updated:', {
        userId: subscription.metadata?.user_id,
        status: subscription.status,
      })
      // TODO: Call Gateway API to update subscription status
      break
    }

    case 'customer.subscription.deleted': {
      const subscription = event.data.object as Stripe.Subscription
      console.log('Subscription deleted:', {
        userId: subscription.metadata?.user_id,
      })
      // TODO: Call Gateway API to downgrade user to free plan
      break
    }
  }

  return NextResponse.json({ received: true }, { status: 200 })
}
