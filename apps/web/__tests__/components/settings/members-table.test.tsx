import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { MembersTable } from '@/components/settings/members-table'

/**
 * The members table (ENT-202).
 *
 * What is worth testing here is not the markup, it is the claim the component
 * makes about itself: owner-only controls are hidden from everyone else as a
 * courtesy, never as protection. The server refuses the write regardless, so
 * these assertions are about what a viewer is asked to look at, not about what
 * they are prevented from doing.
 *
 * The fallback chain is the other half. display_name is absent when the
 * authorization server returned no name claim, and both name and address are
 * absent for someone invited who has not yet signed in, so a row that renders
 * nothing is a real state rather than a hypothetical one.
 */

vi.mock('@/app/(authed)/o/[org]/settings/actions', () => ({
  idle: { status: 'idle', message: '' },
  updateMemberRoleAction: vi.fn(),
  removeMemberAction: vi.fn(),
}))

const members = [
  {
    userId: 'u-1',
    role: 'owner',
    displayName: 'Ada Lovelace',
    email: 'ada@example.com',
  },
  { userId: 'u-2', role: 'viewer', email: 'miko@example.com' },
  { userId: 'u-3', role: 'member' },
]

describe('what an owner sees', () => {
  it('offers a role control and a removal for every member', () => {
    render(<MembersTable slug="acme" members={members} viewerRole="owner" />)

    expect(screen.getAllByRole('combobox')).toHaveLength(3)
    expect(screen.getAllByRole('button', { name: /remove/i })).toHaveLength(3)
  })
})

describe('what a viewer sees', () => {
  it('sees the people and none of the controls', () => {
    render(<MembersTable slug="acme" members={members} viewerRole="viewer" />)

    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /remove/i }),
    ).not.toBeInTheDocument()
  })

  // A viewer can read the list at all because ListMembers declares org:read
  // rather than org:manage. If that ever changed, this page would render empty
  // for the people it is most useful to.
  it('still shows every role as text', () => {
    render(<MembersTable slug="acme" members={members} viewerRole="viewer" />)

    expect(screen.getByText('owner')).toBeInTheDocument()
    expect(screen.getByText('member')).toBeInTheDocument()
  })
})

describe('a member with nothing to show', () => {
  it('falls back from name to address to id rather than rendering blank', () => {
    render(<MembersTable slug="acme" members={members} viewerRole="viewer" />)

    // Name present: the name wins and the address is shown beside it.
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText('ada@example.com')).toBeInTheDocument()

    // No name: the address stands in for it.
    expect(screen.getByText('miko@example.com')).toBeInTheDocument()

    // Neither: the id, which is ugly and still better than an empty cell.
    expect(screen.getByText('u-3')).toBeInTheDocument()
  })
})
