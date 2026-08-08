import { beforeEach, describe, expect, it, vi } from 'vitest'

const { generateAndPersistFindingMock } = vi.hoisted(() => ({
  generateAndPersistFindingMock: vi.fn(),
}))

vi.mock('@/lib/analyst/persistence', () => ({
  generateAndPersistFinding: generateAndPersistFindingMock,
}))

import {
  DEFAULT_NARRATE_LIMIT,
  narratePendingFindings,
} from '@/lib/analyst/narrate-sweep'

/**
 * ENT-162 — the sweep that gives the narrative layer a caller.
 *
 * The generator itself is covered by narrative.test.ts; what matters here is
 * the sweep's contract around it: it selects only findings still on the SQL
 * baseline, a critic rejection leaves the baseline in place rather than
 * persisting junk, and one exploding finding does not abort the rest.
 */

interface QueryState {
  filters: Record<string, unknown>
  limit?: number
  ordered?: string
}

/** Minimal thenable stand-in for the Supabase query builder. */
function fakeSupabase(rows: { id: string }[], error: { message: string } | null = null) {
  const state: QueryState = { filters: {} }
  const builder: Record<string, unknown> = {}
  const chain = (k: string, v: unknown) => {
    state.filters[k] = v
    return builder
  }
  Object.assign(builder, {
    select: () => builder,
    eq: (col: string, val: unknown) => chain(col, val),
    is: (col: string, val: unknown) => chain(`is:${col}`, val),
    order: (col: string) => {
      state.ordered = col
      return builder
    },
    limit: (n: number) => {
      state.limit = n
      return Promise.resolve({ data: rows, error })
    },
  })
  return {
    state,
    client: { from: () => builder } as never,
  }
}

const OK = { ok: true, narrative: { description: 'd', proposedAction: 'a' }, reasons: [], attempts: 1 }
const REJECTED = { ok: false, reasons: ['generic_verb'], attempts: 2 }

beforeEach(() => generateAndPersistFindingMock.mockReset())

describe('narratePendingFindings (ENT-162)', () => {
  it('narrates every finding still carrying the SQL baseline', async () => {
    generateAndPersistFindingMock.mockResolvedValue(OK)
    const { client } = fakeSupabase([{ id: 'f1' }, { id: 'f2' }])

    const summary = await narratePendingFindings({ supabase: client })

    expect(summary).toEqual({ processed: 2, narrated: 2, skipped: 0, failed: 0 })
    expect(generateAndPersistFindingMock).toHaveBeenCalledTimes(2)
    expect(generateAndPersistFindingMock.mock.calls.map((c) => c[1])).toEqual(['f1', 'f2'])
  })

  it('selects only pending findings that have no narrative yet', async () => {
    generateAndPersistFindingMock.mockResolvedValue(OK)
    const { client, state } = fakeSupabase([])

    await narratePendingFindings({ supabase: client })

    expect(state.filters['status']).toBe('pending')
    expect(state.filters['is:narrative_generated_at']).toBeNull()
    expect(state.limit).toBe(DEFAULT_NARRATE_LIMIT)
  })

  it('counts a critic rejection as skipped and leaves the baseline alone', async () => {
    // generateAndPersistFinding only writes when the critic passes, so "skipped"
    // means the finding still reads as its baseline and will be retried later.
    generateAndPersistFindingMock.mockResolvedValue(REJECTED)
    const { client } = fakeSupabase([{ id: 'f1' }])

    const summary = await narratePendingFindings({ supabase: client })

    expect(summary).toEqual({ processed: 1, narrated: 0, skipped: 1, failed: 0 })
  })

  it('keeps going when one finding throws', async () => {
    generateAndPersistFindingMock
      .mockRejectedValueOnce(new Error('model outage'))
      .mockResolvedValueOnce(OK)
    const { client } = fakeSupabase([{ id: 'boom' }, { id: 'fine' }])

    const summary = await narratePendingFindings({ supabase: client })

    expect(summary).toEqual({ processed: 2, narrated: 1, skipped: 0, failed: 1 })
  })

  it('honours an explicit limit and user scope', async () => {
    generateAndPersistFindingMock.mockResolvedValue(OK)
    const { client, state } = fakeSupabase([])

    await narratePendingFindings({ supabase: client, limit: 5, userId: 'u1' })

    expect(state.limit).toBe(5)
    expect(state.filters['user_id']).toBe('u1')
  })

  it('surfaces a query failure rather than reporting a clean sweep', async () => {
    const { client } = fakeSupabase([], { message: 'permission denied' })

    await expect(narratePendingFindings({ supabase: client })).rejects.toThrow(/permission denied/)
  })
})
