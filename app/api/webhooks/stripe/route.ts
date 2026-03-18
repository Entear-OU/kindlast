import { NextResponse } from 'next/server'
import Stripe from 'stripe'
import { createServiceRoleClient } from '@/lib/supabase/service-role'
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
  } catch (error) {
    return NextResponse.json(
      { error: 'Invalid signature' },
      { status: 400 }
    )
  }

  const supabase = createServiceRoleClient()

  switch (event.type) {
    case 'checkout.session.completed': {
      const session = event.data.object as Stripe.Checkout.Session
      const userId = session.metadata?.user_id

      if (userId) {
        await supabase.from('subscriptions').upsert(
          {
            user_id: userId,
            stripe_customer_id: session.customer as string,
            stripe_subscription_id: session.subscription as string,
            plan: 'premium',
            status: 'active',
          },
          { onConflict: 'user_id' }
        )
      }
      break
    }

    case 'customer.subscription.updated': {
      const subscription = event.data.object as Stripe.Subscription
      const userId = subscription.metadata?.user_id
      const currentPeriodEnd = (subscription as unknown as Record<string, unknown>).current_period_end as number | undefined

      if (userId) {
        await supabase
          .from('subscriptions')
          .update({
            status: subscription.status,
            ...(currentPeriodEnd && {
              current_period_end: new Date(
                currentPeriodEnd * 1000
              ).toISOString(),
            }),
          })
          .eq('user_id', userId)
      }
      break
    }

    case 'customer.subscription.deleted': {
      const subscription = event.data.object as Stripe.Subscription
      const userId = subscription.metadata?.user_id

      if (userId) {
        await supabase
          .from('subscriptions')
          .update({
            plan: 'free',
            status: 'canceled',
            stripe_subscription_id: null,
          })
          .eq('user_id', userId)
      }
      break
    }
  }

  return NextResponse.json({ received: true }, { status: 200 })
}
