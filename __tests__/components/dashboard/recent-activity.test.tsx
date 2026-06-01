import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { RecentActivity } from '@/components/dashboard/recent-activity'
import type { AuditEntry, RecentActivity as RecentActivityData } from '@/lib/dashboard/activity'

/**
 * ENT-80 — the recent-actions + last-Watcher-run widget: the prominent run
 * timestamp, the >36h stale warning, and the audit trail rows.
 */

function entry(over: Partial<AuditEntry> = {}): AuditEntry {
  return {
    id: 'a1',
    actionType: 'create_ropa',
    targetTable: 'processing_activities',
    targetId: 't1',
    approvingUserId: 'u1',
    occurredAt: new Date().toISOString(),
    ...over,
  }
}

function activity(over: Partial<RecentActivityData> = {}): RecentActivityData {
  return { entries: [], watcherLastRunAt: new Date().toISOString(), ...over }
}

describe('RecentActivity (ENT-80)', () => {
  it('shows the last Watcher run prominently', () => {
    render(<RecentActivity activity={activity()} currentUserId="u1" />)
    expect(screen.getByText('Last Watcher run')).toBeInTheDocument()
  })

  it('warns when the last run is stale (over 36 hours)', () => {
    // A run from 2020 is unambiguously older than 36 hours regardless of clock.
    render(
      <RecentActivity
        activity={activity({ watcherLastRunAt: '2020-01-01T00:00:00.000Z' })}
        currentUserId="u1"
      />,
    )
    expect(screen.getByRole('status')).toHaveTextContent(/over 36 hours/i)
  })

  it('warns when the Watcher has never run', () => {
    render(
      <RecentActivity activity={activity({ watcherLastRunAt: null })} currentUserId="u1" />,
    )
    expect(screen.getByRole('status')).toHaveTextContent(/hasn't run yet/i)
  })

  it('does not warn for a fresh run', () => {
    render(<RecentActivity activity={activity()} currentUserId="u1" />)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('renders each action with its label, target and approver', () => {
    render(
      <RecentActivity
        activity={activity({ entries: [entry()] })}
        currentUserId="u1"
        currentUserEmail="founder@acme.io"
      />,
    )
    expect(screen.getByText(/Created processing record/)).toBeInTheDocument()
    expect(screen.getByText(/Records of processing/)).toBeInTheDocument()
    expect(screen.getByText(/by founder@acme.io/)).toBeInTheDocument()
  })

  it('shows an empty state when there are no actions', () => {
    render(<RecentActivity activity={activity({ entries: [] })} currentUserId="u1" />)
    expect(screen.getByText(/No actions yet/i)).toBeInTheDocument()
  })
})
