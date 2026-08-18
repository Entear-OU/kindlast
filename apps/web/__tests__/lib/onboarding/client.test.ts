import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

import {
  answerQuestion,
  getOnboardingSession,
  hasComplianceProfile,
} from '@/lib/onboarding/client'

/**
 * The onboarding client (ENT-212).
 *
 * Two properties, and the second is the one worth the file.
 *
 * The first is that an answer travels verbatim. Nothing in the browser parses
 * "Ireland, Spain" into a list, because a second implementation of that rule is
 * a second thing to be wrong, and the day the two disagree a profile holds
 * something nobody typed.
 *
 * The second is the direction the profile gate fails in. `hasComplianceProfile`
 * decides whether a signed-in person is routed out of the console and into
 * onboarding. If it treated an unreachable core-api as "no profile", a routine
 * outage would tell every customer their organisation had been reset and put
 * them in an interview they had already done. Failing open leaves them on a
 * page that says the workspace is unavailable, which is true and recovers by
 * itself.
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

function ok(body: unknown) {
  return { ok: true, status: 200, json: async () => body }
}

describe('an answer travels as the person typed it', () => {
  it('sends the words and the key, and parses nothing', async () => {
    fetchMock.mockResolvedValue(ok({ state: {} }))

    await answerQuestion(
      'token',
      'org-1',
      'PROFILE_FACT_KEY_EU_JURISDICTIONS',
      'Ireland, Spain and Portugal',
    )

    const [, init] = fetchMock.mock.calls[0]
    const body = JSON.parse(init.body as string)
    expect(body.key).toBe('PROFILE_FACT_KEY_EU_JURISDICTIONS')
    expect(body.answer).toBe('Ireland, Spain and Portugal')
    // No `value`, no list, no splitting. What it means is core-api's decision.
    expect(body.value).toBeUndefined()
    expect(body.skip).toBe(false)
  })

  it('sends a skip as a skip', async () => {
    fetchMock.mockResolvedValue(ok({ state: {} }))

    await answerQuestion('token', 'org-1', 'PROFILE_FACT_KEY_HAS_DPO', '', true)

    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.skip).toBe(true)
  })
})

describe('the profile gate fails open', () => {
  it('reports a profile when core-api cannot be reached', async () => {
    fetchMock.mockRejectedValue(new Error('connect ECONNREFUSED'))

    // The direction matters more than the value. Bouncing a signed-in person
    // into onboarding during an outage is worse than letting them reach a page
    // that tells them the truth.
    expect(await hasComplianceProfile('token', 'org-1')).toBe(true)
  })

  it('reports no profile only when core-api says so', async () => {
    fetchMock.mockResolvedValue(ok({ state: { profileExists: false } }))
    expect(await hasComplianceProfile('token', 'org-1')).toBe(false)

    fetchMock.mockResolvedValue(ok({ state: { profileExists: true } }))
    expect(await hasComplianceProfile('token', 'org-1')).toBe(true)
  })

  it('reads an omitted field as false, because that is what proto3 sends', async () => {
    // NOT A DEFENSIVE CASE. Connect's JSON omits a proto3 field at its zero
    // value, so an organisation with no profile answers `{"state":{}}` and the
    // flag is simply not there. Treating an absent flag as "unknown, assume
    // profiled" would mean the gate never fires for the exact organisations it
    // exists for, and it would look correct in every response that had one.
    fetchMock.mockResolvedValue(ok({ state: {} }))
    expect(await hasComplianceProfile('token', 'org-1')).toBe(false)
  })
})

describe('the organisation is a header, never a body field', () => {
  it('names the organisation in the tenancy header', async () => {
    fetchMock.mockResolvedValue(ok({ state: {} }))

    await getOnboardingSession('token', 'org-1')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain(
      'kindlast.core.v1.OnboardingService/GetOnboardingSession',
    )
    expect(init.headers['Kindlast-Org-Id']).toBe('org-1')
    expect(init.headers.Authorization).toBe('Bearer token')
  })
})
