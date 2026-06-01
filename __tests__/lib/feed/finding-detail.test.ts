import { describe, expect, it } from 'vitest'

import { FEED_SEVERITIES } from '@/lib/feed/findings'
import {
  gateChunks,
  severityRationale,
  type SupportingChunk,
} from '@/lib/feed/finding-detail'

/**
 * ENT-64 — pure helpers behind the finding detail view: the free-tier chunk
 * gate and the severity rationale. The Supabase loaders (loadFindingDetail,
 * loadSupportingChunks) are covered by the integration suite.
 */

function chunk(over: Partial<SupportingChunk> = {}): SupportingChunk {
  return {
    ordinal: 0,
    label: 'GDPR Art. 28(3)',
    quoted_text: 'Processing by a processor shall be governed by a contract...',
    source_url: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_28',
    ...over,
  }
}

describe('gateChunks (ENT-64)', () => {
  const chunks = [
    chunk({ ordinal: 0, label: 'a' }),
    chunk({ ordinal: 1, label: 'b' }),
    chunk({ ordinal: 2, label: 'c' }),
  ]

  it('shows all chunks with nothing locked for pro', () => {
    expect(gateChunks(chunks, 'pro')).toEqual({ visible: chunks, lockedCount: 0 })
  })

  it('shows only the first chunk and locks the rest for free', () => {
    const gated = gateChunks(chunks, 'free')
    expect(gated.visible.map((c) => c.label)).toEqual(['a'])
    expect(gated.lockedCount).toBe(2)
  })

  it('returns empty visible and zero locked for an empty list', () => {
    expect(gateChunks([], 'pro')).toEqual({ visible: [], lockedCount: 0 })
    expect(gateChunks([], 'free')).toEqual({ visible: [], lockedCount: 0 })
  })

  it('shows the single chunk and locks nothing for a one-chunk list', () => {
    const one = [chunk({ ordinal: 0, label: 'only' })]
    expect(gateChunks(one, 'free')).toEqual({ visible: one, lockedCount: 0 })
    expect(gateChunks(one, 'pro')).toEqual({ visible: one, lockedCount: 0 })
  })
})

describe('severityRationale (ENT-64)', () => {
  it('returns a non-empty distinct string for each severity', () => {
    const seen = new Set<string>()
    for (const s of FEED_SEVERITIES) {
      const rationale = severityRationale(s)
      expect(rationale).toBeTruthy()
      expect(seen.has(rationale)).toBe(false)
      seen.add(rationale)
    }
    expect(seen.size).toBe(FEED_SEVERITIES.length)
  })
})
