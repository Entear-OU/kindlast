import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  listMembers,
  updateMemberRole,
  removeMember,
  renameOrganisation,
} from '@/lib/org/client'

/**
 * The mutation client (ENT-202).
 *
 * The property under test is that failures keep their reason. lib/auth's
 * `call` collapses every failure to null, which is right for reads and wrong
 * here: a settings page must tell "you are not an owner" apart from "core-api
 * is down", because one is a sentence to show and the other is a retry.
 *
 * It is ENT-198's three-outcome resolution applied to writes. That code keeps
 * "not a member" apart from "the call failed" so a core-api outage can never
 * render as a 404 telling someone their organisation does not exist. Designed
 * in rather than learned from an incident, and the same reasoning holds for
 * mutations: null for two different reasons is a lie by omission.
 */

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  vi.stubEnv('KINDLAST_CORE_API_URL', 'http://core-api:8080')
  fetchMock.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
})

function connectError(status: number, code: string, message: string) {
  return {
    ok: false,
    status,
    json: async () => ({ code, message }),
  }
}

describe('a failure keeps its reason', () => {
  const cases = [
    {
      code: 'permission_denied',
      status: 403,
      kind: 'denied',
      why: 'the page shows the sentence rather than offering a retry',
    },
    {
      code: 'not_found',
      status: 404,
      kind: 'missing',
      why: 'the member is gone, which is not the same as being refused',
    },
    {
      code: 'failed_precondition',
      status: 400,
      kind: 'refused',
      why: 'a rule said no, such as removing the last owner',
    },
    {
      code: 'invalid_argument',
      status: 400,
      kind: 'refused',
      why: 'the input was wrong, which the person can fix',
    },
    {
      code: 'internal',
      status: 500,
      kind: 'unavailable',
      why: 'a retry is the only sensible offer',
    },
  ] as const

  for (const c of cases) {
    it(`maps ${c.code} to ${c.kind}, because ${c.why}`, async () => {
      fetchMock.mockResolvedValue(
        connectError(c.status, c.code, 'the server said so'),
      )

      const result = await removeMember('at', 'org-1', 'user-1')

      expect(result.ok).toBe(false)
      if (result.ok) return
      expect(result.error.kind).toBe(c.kind)
      expect(result.error.message).toBe('the server said so')
    })
  }
})

describe('the transport', () => {
  it('sends the organisation header on every mutation', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    })

    await updateMemberRole('at-1', 'org-9', 'user-1', 'member')

    const [, init] = fetchMock.mock.calls[0]
    expect(init.headers['Kindlast-Org-Id']).toBe('org-9')
    expect(init.headers.Authorization).toBe('Bearer at-1')
    expect(JSON.parse(init.body)).toEqual({ userId: 'user-1', role: 'member' })
  })

  // A gateway failing in front of core-api answers HTML, not a Connect error
  // body. Parsing must not turn that into an exception nobody catches.
  it('reports a non-JSON error body as unavailable rather than throwing', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => {
        throw new SyntaxError('Unexpected token <')
      },
    })

    const result = await listMembers('at', 'org-1')

    expect(result.ok).toBe(false)
    if (result.ok) return
    expect(result.error.kind).toBe('unavailable')
    expect(result.error.message).toContain('502')
  })

  it('reports an unreachable core-api as unavailable rather than throwing', async () => {
    fetchMock.mockRejectedValue(new Error('ECONNREFUSED'))

    const result = await renameOrganisation('at', 'org-1', 'New Name')

    expect(result.ok).toBe(false)
    if (result.ok) return
    expect(result.error.kind).toBe('unavailable')
  })

  it('reports a missing base url without attempting a request', async () => {
    vi.stubEnv('KINDLAST_CORE_API_URL', '')

    const result = await listMembers('at', 'org-1')

    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.ok).toBe(false)
    if (result.ok) return
    expect(result.error.kind).toBe('unavailable')
  })
})

describe('success', () => {
  it('returns the value rather than a bare true', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        members: [{ userId: 'u1', role: 'owner', displayName: 'Ada' }],
      }),
    })

    const result = await listMembers('at', 'org-1')

    expect(result.ok).toBe(true)
    if (!result.ok) return
    expect(result.value.members?.[0].displayName).toBe('Ada')
  })
})
