import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-86 — the webhook route. Pins the contract independent of the provider:
 * 400 on a signature failure, 200 (ignored) on an unhandled event, 200 (applied)
 * when a change is written via the service role.
 */

const { parseWebhookMock, applyMock } = vi.hoisted(() => ({
  parseWebhookMock: vi.fn(),
  applyMock: vi.fn(),
}))

vi.mock('@/lib/billing/provider', () => ({
  getBillingProvider: () => ({ name: 'stripe', parseWebhook: parseWebhookMock }),
}))
vi.mock('@/lib/billing/apply', () => ({ applySubscriptionChange: applyMock }))
vi.mock('@/lib/supabase/service-role', () => ({ createServiceRoleClient: () => ({}) }))

import { POST } from '@/app/api/webhooks/billing/route'

function request(body = '{}'): Request {
  return new Request('https://app.test/api/webhooks/billing', {
    method: 'POST',
    body,
    headers: { 'stripe-signature': 'sig' },
  })
}

beforeEach(() => vi.clearAllMocks())

describe('POST /api/webhooks/billing (ENT-86)', () => {
  it('returns 400 when signature verification fails', async () => {
    parseWebhookMock.mockRejectedValue(new Error('signature verification failed'))
    const res = await POST(request())
    expect(res.status).toBe(400)
    expect(applyMock).not.toHaveBeenCalled()
  })

  it('returns 200 and ignores an unhandled event (null change)', async () => {
    parseWebhookMock.mockResolvedValue(null)
    const res = await POST(request())
    expect(res.status).toBe(200)
    expect(await res.json()).toMatchObject({ ignored: true })
    expect(applyMock).not.toHaveBeenCalled()
  })

  it('applies the change and returns 200 when the event is handled', async () => {
    const change = { eventId: 'evt_1', customerId: 'cus_1', plan: 'pro', status: 'active' }
    parseWebhookMock.mockResolvedValue(change)
    applyMock.mockResolvedValue(true)

    const res = await POST(request())
    expect(res.status).toBe(200)
    expect(applyMock).toHaveBeenCalledWith(expect.anything(), change)
    expect(await res.json()).toMatchObject({ received: true, applied: true })
  })

  it('reports applied:false for a replayed (already-processed) event', async () => {
    parseWebhookMock.mockResolvedValue({ eventId: 'evt_dup', customerId: 'c', plan: 'pro', status: 'active' })
    applyMock.mockResolvedValue(false)
    const res = await POST(request())
    expect(await res.json()).toMatchObject({ applied: false })
  })
})
