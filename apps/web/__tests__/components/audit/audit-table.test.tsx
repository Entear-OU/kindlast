import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import { AuditTable } from '@/components/audit/audit-table'

/**
 * The audit table (ENT-223).
 *
 * Two properties matter more than the rest, and both fail silently in a
 * browser: an act by somebody who has left must still be visible, and the role
 * shown must be the recorded one rather than one resolved now.
 */

const at = '2026-08-17T12:32:05Z'

describe('AuditTable', () => {
  it('shows the role recorded at the time', () => {
    render(
      <AuditTable
        entries={[
          {
            id: 'a1',
            occurredAt: at,
            actionType: 'approve_finding',
            actor: {
              userId: 'u1',
              displayName: 'Ada Lovelace',
              actorRole: 'member',
              kind: 'ACTOR_KIND_HUMAN',
            },
          },
        ]}
      />,
    )

    expect(screen.getByText('Approved a finding')).toBeInTheDocument()
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    // Ada may be an owner today. The log has to stay true about the past.
    expect(screen.getByText('member')).toBeInTheDocument()
  })

  it('still shows an act by somebody who has left', () => {
    // A log that dropped rows when somebody was offboarded would be defeatable
    // by offboarding somebody.
    render(
      <AuditTable
        entries={[
          {
            id: 'a1',
            occurredAt: at,
            actionType: 'reject_finding',
            actor: { userId: 'u-gone', actorRole: 'owner' },
          },
        ]}
      />,
    )

    expect(screen.getByText('Rejected a finding')).toBeInTheDocument()
    expect(screen.getByText('A former member')).toBeInTheDocument()
  })

  it('renders an unfamiliar action type as itself', () => {
    // The vocabulary grows as obligations are added. An audit log is the one
    // register where a value the client has not been taught must still show
    // what it says, rather than flattening to "Unknown".
    render(
      <AuditTable
        entries={[
          {
            id: 'a1',
            occurredAt: at,
            actionType: 'retire_ai_system',
            actor: { userId: 'u1', displayName: 'Ada', actorRole: 'owner' },
          },
        ]}
      />,
    )

    expect(screen.getByText('retire_ai_system')).toBeInTheDocument()
    expect(screen.queryByText(/unknown/i)).toBeNull()
  })

  it('says a missing role is not recorded rather than leaving a blank cell', () => {
    // `actor_role` is nullable and rows predating 00002 have none. An empty
    // cell in an audit table reads as a rendering fault.
    render(
      <AuditTable
        entries={[
          {
            id: 'a1',
            occurredAt: at,
            actionType: 'approve_finding',
            actor: { userId: 'u1', displayName: 'Ada' },
          },
        ]}
      />,
    )

    expect(screen.getByText('Not recorded')).toBeInTheDocument()
  })

  it.each([
    ['rename_organisation', 'Renamed the organisation'],
    ['invite_member', 'Invited somebody to join'],
    ['change_member_role', "Changed somebody's role"],
    ['remove_member', 'Removed somebody'],
    ['create_ropa_manual', 'Added an Article 30 entry by hand'],
    ['create_dsar_manual', 'Logged a data subject request by hand'],
    ['update_ropa', 'Edited an Article 30 entry'],
  ])('reads %s as a sentence', (actionType, expected) => {
    // The membership actions, which used to write no row at all. A reader who
    // sees the raw `change_member_role` is reading a schema rather than a
    // record, and these are the rows an auditor asks for first: who was let in,
    // at what authority, and when it was taken away.
    render(
      <AuditTable
        entries={[
          {
            id: 'a1',
            occurredAt: at,
            actionType,
            actor: { userId: 'u1', displayName: 'Ada', actorRole: 'owner' },
          },
        ]}
      />,
    )

    expect(screen.getByText(expected)).toBeInTheDocument()
  })

  it('shows the time in UTC and keeps the machine-readable instant', () => {
    // The page and the export have to be reconcilable without guessing a
    // timezone, and this file is read by people in other offices.
    const { container } = render(
      <AuditTable
        entries={[
          {
            id: 'a1',
            occurredAt: at,
            actionType: 'approve_finding',
            actor: { userId: 'u1', displayName: 'Ada', actorRole: 'owner' },
          },
        ]}
      />,
    )

    const time = container.querySelector('time')
    expect(time).not.toBeNull()
    expect(time?.getAttribute('dateTime')).toBe(at)
    expect(time?.textContent).toContain('2026-08-17 12:32:05 UTC')
  })
})
