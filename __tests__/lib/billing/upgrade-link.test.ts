import { describe, expect, it } from 'vitest'

import { upgradeHref } from '@/lib/billing/upgrade-link'

/**
 * ENT-85 — the upgrade-page href builder. Pins that a returnTo path is encoded
 * into the query (so checkout can land the user back where they were) and that
 * the bare /billing link is used when there's nothing to return to.
 */
describe('upgradeHref (ENT-85)', () => {
  it('encodes returnTo into the query', () => {
    expect(upgradeHref('/feed')).toBe('/billing?returnTo=%2Ffeed')
    expect(upgradeHref('/records/ropa')).toBe('/billing?returnTo=%2Frecords%2Fropa')
  })

  it('falls back to bare /billing with no returnTo', () => {
    expect(upgradeHref()).toBe('/billing')
    expect(upgradeHref('')).toBe('/billing')
  })
})
