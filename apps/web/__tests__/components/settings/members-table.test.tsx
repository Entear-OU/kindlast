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

/**
 * Knowing which row is you (ENT-220).
 *
 * The identifier passed in has to be core-api's derived `userId`, not the IdP
 * subject claim `id`. Getting that wrong fails silently: nothing matches, no
 * row is marked, and leaving disappears without an error anywhere. So these
 * assertions are about the match, and about what happens when it does not
 * happen at all.
 */
describe('marking the viewer own row', () => {
  it('marks exactly one row as you', () => {
    render(
      <MembersTable
        slug="acme"
        members={members}
        viewerRole="owner"
        viewerUserId="u-2"
      />,
    )

    expect(screen.getAllByText('You')).toHaveLength(1)
  })

  it('marks nothing when the id is missing, rather than guessing', () => {
    // The state a deployment is in if `user_id` does not arrive. Marking the
    // first row, or any row, would tell somebody they are looking at
    // themselves when they are not, and offer to remove a stranger.
    render(<MembersTable slug="acme" members={members} viewerRole="owner" />)

    expect(screen.queryByText('You')).not.toBeInTheDocument()
  })

  it('marks nothing when the id matches nobody', () => {
    render(
      <MembersTable
        slug="acme"
        members={members}
        viewerRole="owner"
        viewerUserId="386250729179840515"
      />,
    )

    // What passing the IdP subject claim instead of the derived id looks like.
    expect(screen.queryByText('You')).not.toBeInTheDocument()
  })
})

describe('leaving', () => {
  it('offers leave on your own row and remove on everyone else', () => {
    render(
      <MembersTable
        slug="acme"
        members={members}
        viewerRole="owner"
        viewerUserId="u-2"
      />,
    )

    expect(screen.getByRole('button', { name: /^leave$/i })).toBeInTheDocument()
    // Two others, and crucially not a third: an owner must not be offered
    // "Remove" on themselves, because that is the action that needs a stop.
    expect(screen.getAllByRole('button', { name: /remove/i })).toHaveLength(2)
  })

  it('offers leave to a viewer, who can manage nobody else', () => {
    // Every role may leave: memberships_delete_owner_or_self has always
    // allowed it. Before ENT-220 the page could not offer it to anybody.
    render(
      <MembersTable
        slug="acme"
        members={members}
        viewerRole="viewer"
        viewerUserId="u-2"
      />,
    )

    expect(screen.getByRole('button', { name: /^leave$/i })).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /remove/i }),
    ).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })

  it('does not submit the removal until it is confirmed', () => {
    // The confirmation is the point. Leaving takes your own access away
    // immediately and may not be undoable without another owner, so the
    // destructive control must not be reachable in one click.
    render(
      <MembersTable
        slug="acme"
        members={members}
        viewerRole="owner"
        viewerUserId="u-2"
      />,
    )

    expect(
      screen.queryByRole('button', { name: /leave organisation/i }),
    ).not.toBeInTheDocument()
  })
})
