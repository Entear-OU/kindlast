// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { narrateMock, serviceRoleMock } = vi.hoisted(() => ({
  narrateMock: vi.fn(),
  serviceRoleMock: vi.fn(() => ({ marker: 'service-role' })),
}))

vi.mock('@/lib/analyst/narrate-sweep', () => ({ narratePendingFindings: narrateMock }))
vi.mock('@/lib/supabase/service-role', () => ({ createServiceRoleClient: serviceRoleMock }))

import { GET } from '@/app/api/analyst/narrate/route'

/**
 * ENT-162 — the cron entry point for the narrative sweep.
 *
 * The sweep itself is unit-tested separately; this pins the security contract,
 * which is the part that must not regress: the endpoint writes to every user's
 * findings under the service role, so an unauthenticated caller must never
 * reach it, and a missing secret must fail closed rather than open.
 */

const SECRET = 'test-cron-secret'
const SUMMARY = { processed: 3, narrated: 2, skipped: 1, failed: 0 }

function request(auth?: string): Request {
  return new Request('https://kindlast.test/api/analyst/narrate', {
    headers: auth ? { authorization: auth } : {},
  })
}

beforeEach(() => {
  narrateMock.mockReset()
  narrateMock.mockResolvedValue(SUMMARY)
  process.env.CRON_SECRET = SECRET
})

afterEach(() => {
  delete process.env.CRON_SECRET
})

describe('GET /api/analyst/narrate (ENT-162)', () => {
  it('runs the sweep and returns its summary for an authorised cron call', async () => {
    const res = await GET(request(`Bearer ${SECRET}`))

    expect(res.status).toBe(200)
    await expect(res.json()).resolves.toEqual(SUMMARY)
    expect(narrateMock).toHaveBeenCalledTimes(1)
  })

  it('rejects a call with no Authorization header', async () => {
    const res = await GET(request())

    expect(res.status).toBe(401)
    expect(narrateMock).not.toHaveBeenCalled()
  })

  it('rejects a call carrying the wrong secret', async () => {
    const res = await GET(request('Bearer not-the-secret'))

    expect(res.status).toBe(401)
    expect(narrateMock).not.toHaveBeenCalled()
  })

  it('fails closed when CRON_SECRET is not configured', async () => {
    delete process.env.CRON_SECRET

    const res = await GET(request('Bearer '))

    expect(res.status).toBe(401)
    expect(narrateMock).not.toHaveBeenCalled()
  })

  it('sweeps under the service role, not a user client', async () => {
    await GET(request(`Bearer ${SECRET}`))

    expect(serviceRoleMock).toHaveBeenCalled()
    expect(narrateMock.mock.calls[0][0].supabase).toEqual({ marker: 'service-role' })
  })
})
