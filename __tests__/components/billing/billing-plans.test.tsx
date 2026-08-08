import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BillingPlans } from '@/components/billing/billing-plans'

/**
 * ENT-85 — the pricing surface. Pins: Free shows an upgrade CTA that starts
 * checkout (with returnTo) and redirects to the returned URL; an error surfaces
 * without redirecting; Pro shows "Current plan" and no CTA.
 */

const { startCheckoutMock } = vi.hoisted(() => ({ startCheckoutMock: vi.fn() }))
vi.mock('@/app/(authed)/billing/actions', () => ({ startCheckout: startCheckoutMock }))

let locationStub: { href: string }

beforeEach(() => {
  startCheckoutMock.mockReset()
  locationStub = { href: '' }
  Object.defineProperty(window, 'location', { value: locationStub, writable: true })
})
afterEach(() => vi.clearAllMocks())

describe('BillingPlans (ENT-85)', () => {
  it('starts checkout with returnTo and redirects to the checkout url', async () => {
    const user = userEvent.setup()
    startCheckoutMock.mockResolvedValue({ ok: true, url: 'https://checkout.stripe/x' })
    render(<BillingPlans plan="free" returnTo="/records/ropa" />)

    await user.click(screen.getByRole('button', { name: /upgrade for €49\/month/i }))

    expect(startCheckoutMock).toHaveBeenCalledWith('/records/ropa')
    await waitFor(() => expect(locationStub.href).toBe('https://checkout.stripe/x'))
  })

  it('surfaces an error without redirecting', async () => {
    const user = userEvent.setup()
    startCheckoutMock.mockResolvedValue({ ok: false, error: 'Checkout failed' })
    render(<BillingPlans plan="free" />)

    await user.click(screen.getByRole('button', { name: /upgrade/i }))

    expect(await screen.findByText('Checkout failed')).toBeInTheDocument()
    expect(locationStub.href).toBe('')
  })

  it('shows the current-plan state for Pro with no upgrade CTA', () => {
    render(<BillingPlans plan="pro" />)
    expect(screen.getByText(/you’re on pro/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /upgrade/i })).not.toBeInTheDocument()
    expect(screen.getAllByText(/current plan/i).length).toBeGreaterThan(0)
  })
})
