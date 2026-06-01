import { describe, expect, it } from 'vitest'

import {
  deriveRopaStatus,
  formatUpdatedAt,
  manualActivityCount,
  type ProcessingActivity,
} from '@/lib/records/ropa'

/**
 * ENT-70 — pure helpers behind the ROPA register: the derived status pill, the
 * manual-activity count (for the Free-tier cap), and the "last updated" label.
 */

const base: ProcessingActivity = {
  id: 'a1',
  name: 'Email marketing',
  purpose: 'Send newsletters',
  legal_basis: 'consent',
  data_categories: ['email', 'name'],
  recipients: ['Mailchimp'],
  retention_period: '24 months',
  finding_id: null,
  created_at: '2026-05-01T10:00:00.000Z',
  updated_at: '2026-05-01T10:00:00.000Z',
}

describe('deriveRopaStatus (ENT-70)', () => {
  it('is incomplete when a mandatory field is missing', () => {
    expect(deriveRopaStatus({ ...base, retention_period: null })).toBe('incomplete')
    expect(deriveRopaStatus({ ...base, data_categories: [] })).toBe('incomplete')
    expect(deriveRopaStatus({ ...base, recipients: [] })).toBe('incomplete')
    expect(deriveRopaStatus({ ...base, legal_basis: '  ' })).toBe('incomplete')
  })

  it('needs review when Executor-prefilled and not yet edited', () => {
    const prefilled: ProcessingActivity = {
      ...base,
      finding_id: 'f1',
      created_at: '2026-05-01T10:00:00.000Z',
      updated_at: '2026-05-01T10:00:00.000Z', // untouched since creation
    }
    expect(deriveRopaStatus(prefilled)).toBe('review_needed')
  })

  it('is complete once a prefilled row has been edited', () => {
    const edited: ProcessingActivity = {
      ...base,
      finding_id: 'f1',
      updated_at: '2026-05-02T09:00:00.000Z', // later than created_at
    }
    expect(deriveRopaStatus(edited)).toBe('complete')
  })

  it('is complete for a fully-filled manual activity', () => {
    expect(deriveRopaStatus(base)).toBe('complete')
  })
})

describe('manualActivityCount (ENT-70)', () => {
  it('counts only rows with no originating finding', () => {
    const rows: ProcessingActivity[] = [
      { ...base, id: '1', finding_id: null },
      { ...base, id: '2', finding_id: 'f' },
      { ...base, id: '3', finding_id: null },
    ]
    expect(manualActivityCount(rows)).toBe(2)
  })
})

describe('formatUpdatedAt (ENT-70)', () => {
  const now = new Date('2026-05-20T12:00:00.000Z')

  it('renders Today for the current day', () => {
    expect(formatUpdatedAt('2026-05-20T08:00:00.000Z', now)).toBe('Today')
  })

  it('renders day + month within the same year', () => {
    expect(formatUpdatedAt('2026-05-08T08:00:00.000Z', now)).toBe('8 May')
  })

  it('includes the year for an older date', () => {
    expect(formatUpdatedAt('2025-12-08T08:00:00.000Z', now)).toBe('8 Dec 2025')
  })

  it('falls back to a dash for an invalid date', () => {
    expect(formatUpdatedAt('not-a-date', now)).toBe('—')
  })
})
