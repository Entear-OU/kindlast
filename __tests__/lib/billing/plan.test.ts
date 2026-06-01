import { describe, expect, it } from 'vitest'

import { getPlan } from '@/lib/billing/plan'

/**
 * The billing seam (ENT-63). Until the subscriptions table lands (ENT-81), every
 * user is on Pro so the approve path works end-to-end; this test pins that
 * contract so the day it changes is a deliberate edit, not a silent regression.
 */
describe('getPlan (billing seam)', () => {
  it('returns "pro" for any user until billing exists', async () => {
    await expect(getPlan('any-user-id')).resolves.toBe('pro')
  })
})
