import { describe, it, expect } from 'vitest'

import { isFiltered, toExportQuery, toFilter } from '@/lib/audit/filter'

/**
 * The audit filter as it travels in a URL (ENT-223).
 *
 * The assertions worth having are the ones where a wrong answer still looks
 * like a working page: a date range that quietly excludes the last day somebody
 * asked for, and an export that quietly exports something other than what is on
 * screen.
 */

describe('toFilter', () => {
  it('includes the whole of the day chosen as the end of the range', () => {
    // The RPC's upper bound is exclusive, and a person choosing 17 August means
    // the whole of the 17th. Sending `2026-08-17T00:00:00Z` as `until` would
    // silently drop every decision made that day, which is exactly the day
    // somebody investigating an incident cares about.
    const filter = toFilter({ since: '2026-08-01', until: '2026-08-17' })

    expect(filter.since).toBe('2026-08-01T00:00:00.000Z')
    expect(filter.until).toBe('2026-08-18T00:00:00.000Z')
  })

  it('reads both ends in UTC rather than the viewer timezone', () => {
    // Two colleagues in different offices opening the same link must see the
    // same rows. A range that means something different depending on who opens
    // it is not a range.
    const filter = toFilter({ since: '2026-01-01' })
    expect(filter.since).toBe('2026-01-01T00:00:00.000Z')
  })

  it('ignores a date it cannot parse rather than sending a bad range', () => {
    // A hand-edited URL should degrade to an unfiltered view, not to an error
    // page and not to an invalid instant the RPC will refuse.
    const filter = toFilter({ since: 'yesterday', until: '2026-13-45' })
    expect(filter.since).toBeUndefined()
    expect(filter.until).toBeUndefined()
  })

  it('takes several action types from repeated parameters', () => {
    const filter = toFilter({ action: ['approve_finding', 'reject_finding'] })
    expect(filter.actionTypes).toEqual(['approve_finding', 'reject_finding'])
  })

  it('leaves an absent filter absent rather than sending an empty array', () => {
    // An empty array reaches the query as `= any('{}')`, which matches no row,
    // so an unfiltered request would come back empty.
    const filter = toFilter({})
    expect(filter.actionTypes).toBeUndefined()
    expect(filter.actorUserIds).toBeUndefined()
    expect(filter.query).toBeUndefined()
  })

  it('drops whitespace-only search text', () => {
    expect(toFilter({ q: '   ' }).query).toBeUndefined()
  })
})

describe('toExportQuery', () => {
  it('carries every filter the page is showing', () => {
    // "Export what I am looking at" has to be true by construction. An export
    // that quietly differs from the table above it is a bug an auditor finds
    // after filing the report.
    const query = toExportQuery({
      since: '2026-08-01',
      until: '2026-08-17',
      action: ['approve_finding', 'reject_finding'],
      actor: ['u1'],
      q: 'ada',
    })

    const params = new URLSearchParams(query)
    expect(params.get('since')).toBe('2026-08-01')
    expect(params.get('until')).toBe('2026-08-17')
    expect(params.getAll('action')).toEqual([
      'approve_finding',
      'reject_finding',
    ])
    expect(params.getAll('actor')).toEqual(['u1'])
    expect(params.get('q')).toBe('ada')
  })

  it('drops the page cursor', () => {
    // An export is the whole matching set. Carrying a cursor into it would be
    // asking for "everything, starting from page three", which nobody means.
    const query = toExportQuery({ since: '2026-08-01', page: 'abc123' })
    expect(new URLSearchParams(query).get('page')).toBeNull()
  })
})

describe('isFiltered', () => {
  it('separates an empty log from a filter that matched nothing', () => {
    // "Nothing has been decided yet" and "no rows match" are different
    // sentences, and only one of them should worry the reader.
    expect(isFiltered({})).toBe(false)
    expect(isFiltered({ page: 'abc' })).toBe(false)
    expect(isFiltered({ q: '  ' })).toBe(false)

    expect(isFiltered({ q: 'ada' })).toBe(true)
    expect(isFiltered({ since: '2026-08-01' })).toBe(true)
    expect(isFiltered({ action: ['approve_finding'] })).toBe(true)
  })
})
