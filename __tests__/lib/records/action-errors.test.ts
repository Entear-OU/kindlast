import { afterEach, describe, expect, it, vi } from 'vitest'

import { recordsActionError } from '@/lib/records/action-errors'

/**
 * ENT-166 — no raw database text reaches the founder.
 *
 * The regression this pins: submitting the ROPA form before onboarding
 * rendered "create_processing_activity: no compliance profile for user" as UI
 * copy. Known conditions get actionable plain language; anything unrecognised
 * collapses to one generic line, so the default is safe rather than leaky.
 */

afterEach(() => vi.restoreAllMocks())

describe('recordsActionError (ENT-166)', () => {
  it('turns the missing-profile exception into a next step', () => {
    const msg = recordsActionError('create_processing_activity: no compliance profile for user')

    expect(msg).toMatch(/finish onboarding/i)
    expect(msg).not.toMatch(/create_processing_activity/)
  })

  it('explains a plan cap as an upgrade, not a failure', () => {
    expect(recordsActionError('free plan cap reached for processing activities')).toMatch(
      /free plan limit/i,
    )
  })

  it('does not echo an unrecognised database error', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})

    const raw = 'duplicate key value violates unique constraint "processing_activities_pkey"'
    const msg = recordsActionError(raw)

    expect(msg).not.toContain('processing_activities_pkey')
    expect(msg).not.toContain('duplicate key')
    expect(msg).toMatch(/couldn't save that/i)
  })

  it('logs the unrecognised detail server-side instead', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})

    recordsActionError('some novel postgres failure', 'addActivity')

    expect(spy).toHaveBeenCalledTimes(1)
    expect(String(spy.mock.calls[0][0])).toContain('some novel postgres failure')
    expect(String(spy.mock.calls[0][0])).toContain('addActivity')
  })

  it('never returns an empty message', () => {
    for (const raw of ['', 'x', 'no compliance profile for user']) {
      expect(recordsActionError(raw).length).toBeGreaterThan(10)
    }
  })
})
