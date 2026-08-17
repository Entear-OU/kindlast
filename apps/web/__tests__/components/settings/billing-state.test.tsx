import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import { BillingState } from '@/components/settings/billing-state'

/**
 * Billing (ENT-210).
 *
 * The assertions worth having are the ones that fail silently in the browser.
 * A `past_due` subscription renders a plausible page whichever way this goes,
 * and the wrong way tells a paying customer they downgraded themselves. A
 * self-hosted deployment renders a plausible page too, and the wrong way
 * advertises a plan to somebody who has no provider to buy it from.
 */

const hosted = { billingConfigured: true, gatingEnabled: true }

describe('BillingState', () => {
  it('says a payment failed rather than only showing the free plan', () => {
    // `plan` is entitlement, so core-api has already reported `free` for a pro
    // subscription whose card was declined. Showing that alone is the bug.
    render(
      <BillingState
        billing={{ ...hosted, plan: 'free', status: 'past_due' }}
      />,
    )

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent(
      /payment for this organisation has not gone through/i,
    )
    expect(alert).toHaveTextContent(
      /nothing in your compliance record has been removed/i,
    )
  })

  it('distinguishes a cancellation from a failed payment', () => {
    render(
      <BillingState
        billing={{ ...hosted, plan: 'free', status: 'canceled' }}
      />,
    )

    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByRole('status')).toHaveTextContent(
      /subscription is cancelled/i,
    )
    // The promise that matters to a compliance customer: a lapsed plan does not
    // hold their record hostage.
    expect(screen.getByRole('status')).toHaveTextContent(
      /readable\s+and exportable/i,
    )
  })

  it('offers no upgrade path on a self-hosted deployment', () => {
    render(
      <BillingState
        billing={{
          plan: 'free',
          billingConfigured: false,
          gatingEnabled: false,
        }}
      />,
    )

    expect(
      screen.getByText(/self-hosted and sells nothing/i),
    ).toBeInTheDocument()
    // The free-plan cap copy would be a lie here: nothing is capped.
    expect(screen.queryByText(/Article 30 entries are capped/i)).toBeNull()
  })

  it('does not advertise a cap when gating is off', () => {
    render(
      <BillingState
        billing={{
          plan: 'free',
          billingConfigured: true,
          gatingEnabled: false,
        }}
      />,
    )

    expect(
      screen.getByText(/not being applied on this deployment/i),
    ).toBeInTheDocument()
    expect(screen.queryByText(/Article 30 entries are capped/i)).toBeNull()
  })

  it('explains the cap on free, and exempts executor records from it', () => {
    render(<BillingState billing={{ ...hosted, plan: 'free', status: '' }} />)

    const copy = screen.getByText(/Article 30 entries are capped/i)
    expect(copy).toHaveTextContent(
      /Executor creates from an approved finding are never/i,
    )
  })

  it('says nothing about caps on a healthy paid plan', () => {
    render(
      <BillingState
        billing={{
          ...hosted,
          plan: 'pro',
          status: 'active',
          currentPeriodEnd: '2026-09-01T00:00:00Z',
        }}
      />,
    )

    expect(screen.getByText('pro')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText(/Article 30 entries are capped/i)).toBeNull()
  })

  it('reads free when core-api reported no plan at all', () => {
    // An organisation that has never bought anything has no subscription row.
    // Rendering an empty plan name would read as a page that failed to load.
    render(<BillingState billing={hosted} />)

    expect(screen.getByText('free')).toBeInTheDocument()
  })
})
