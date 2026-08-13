import { beforeEach, describe, expect, it, vi } from 'vitest'

const { trackMock } = vi.hoisted(() => ({ trackMock: vi.fn() }))
vi.mock('@vercel/analytics', () => ({ track: trackMock }))

import { trackUpgradeConverted, trackUpgradePromptShown } from '@/lib/analytics/track'

/**
 * ENT-82 — the upgrade-funnel tracking wrapper. Pins the event names and the
 * primitive-only payload (Vercel `track` rejects nested/undefined values), and
 * confirms optional counts are dropped rather than sent as undefined.
 */
describe('upgrade tracking (ENT-82)', () => {
  beforeEach(() => trackMock.mockClear())

  it('emits upgrade_prompt_shown with source + counts', () => {
    trackUpgradePromptShown({ source: 'finding_cap', lockedCount: 2, totalCount: 5 })
    expect(trackMock).toHaveBeenCalledWith('upgrade_prompt_shown', {
      source: 'finding_cap',
      lockedCount: 2,
      totalCount: 5,
    })
  })

  it('emits upgrade_prompt_converted with source + counts', () => {
    trackUpgradeConverted({ source: 'finding_cap', lockedCount: 2, totalCount: 5 })
    expect(trackMock).toHaveBeenCalledWith('upgrade_prompt_converted', {
      source: 'finding_cap',
      lockedCount: 2,
      totalCount: 5,
    })
  })

  it('omits undefined counts from the payload', () => {
    trackUpgradePromptShown({ source: 'executor_approve' })
    expect(trackMock).toHaveBeenCalledWith('upgrade_prompt_shown', {
      source: 'executor_approve',
    })
  })
})
