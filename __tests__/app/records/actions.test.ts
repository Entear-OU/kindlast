import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-168 — the manual "add" server actions in Compliance records refuse a
 * record with no identifying field.
 *
 * A blank submit used to reach the RPC, where the migrations coalesce an empty
 * name to a placeholder ("Untitled activity" / "Untitled system"). That
 * fallback is right for Executor-written rows and wrong for a founder pressing
 * save on an empty form: the DSAR case even started a live 30-day Article 12(3)
 * countdown for a request that does not exist. These pin the refusal, and that
 * nothing is written when it fires.
 */

const { getUserMock, rpcMock } = vi.hoisted(() => ({
  getUserMock: vi.fn(),
  rpcMock: vi.fn(),
}))

vi.mock('@/lib/supabase/server', () => ({
  createClient: async () => ({
    auth: { getUser: getUserMock },
    rpc: rpcMock,
  }),
}))

vi.mock('next/cache', () => ({ revalidatePath: vi.fn() }))

import { addActivity, editActivity } from '@/app/(authed)/records/ropa/actions'
import { logDsar } from '@/app/(authed)/records/dsar/actions'
import { addSystem, editSystem } from '@/app/(authed)/records/ai-systems/actions'

const ROPA = {
  name: 'Payroll processing',
  purpose: 'Pay staff',
  legal_basis: 'Contract',
  data_categories: ['names'],
  recipients: ['accountant'],
  retention_period: '7 years',
}

const SYSTEM = {
  name: 'Engagement scoring model',
  vendor: 'in-house',
  purpose: 'Score engagement',
  risk_classification: 'high' as const,
  documentation_status: 'missing' as const,
}

const DSAR = {
  subject_name: 'A. Tamm',
  request_type: 'access',
  handler: 'founder',
}

beforeEach(() => {
  vi.clearAllMocks()
  getUserMock.mockResolvedValue({ data: { user: { id: 'u1' } } })
  rpcMock.mockResolvedValue({ error: null })
})

describe('addActivity (ENT-168)', () => {
  it('creates the activity when it has a name', async () => {
    const res = await addActivity(ROPA)
    expect(res).toEqual({ ok: true })
    expect(rpcMock).toHaveBeenCalledTimes(1)
  })

  it('refuses a blank name and writes nothing', async () => {
    const res = await addActivity({ ...ROPA, name: '' })
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })

  it('refuses a whitespace-only name', async () => {
    const res = await addActivity({ ...ROPA, name: '   ' })
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })

  it('refuses blanking the name through an edit', async () => {
    const res = await editActivity('a1', { ...ROPA, name: '' })
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })
})

describe('logDsar (ENT-168)', () => {
  it('logs the request when it names a requester', async () => {
    const res = await logDsar(DSAR)
    expect(res).toEqual({ ok: true })
    expect(rpcMock).toHaveBeenCalledTimes(1)
  })

  it('refuses a blank requester, so no phantom deadline is started', async () => {
    const res = await logDsar({ ...DSAR, subject_name: '' })
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })

  it('refuses a whitespace-only requester', async () => {
    const res = await logDsar({ ...DSAR, subject_name: '  ' })
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })
})

describe('addSystem (ENT-168)', () => {
  it('registers the system when it has a name', async () => {
    const res = await addSystem(SYSTEM, true)
    expect(res).toEqual({ ok: true })
    expect(rpcMock).toHaveBeenCalledTimes(1)
  })

  it('refuses a blank name and writes nothing', async () => {
    const res = await addSystem({ ...SYSTEM, name: '' }, true)
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })

  it('refuses blanking the name through an edit', async () => {
    const res = await editSystem('s1', { ...SYSTEM, name: '   ' }, true)
    expect(res.ok).toBe(false)
    expect(rpcMock).not.toHaveBeenCalled()
  })
})
