import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { InviteForm } from '@/components/settings/invite-form'

/**
 * The invite form (ENT-219).
 *
 * This control was deliberately absent until an invitation could actually be
 * delivered, because an owner clicking it would have got a created invitation,
 * no email, and no way to learn that the person they invited will never hear
 * anything. The raw token exists for one handler and only its hash is stored,
 * so nothing could have recovered it afterwards.
 *
 * What is worth testing now is the part that would quietly cause harm: the
 * default role. `owner` sorts first because the list reads as a hierarchy, and
 * a form defaulting to the first item is one where a distracted person grants
 * ownership of a compliance workspace by pressing enter.
 */

vi.mock('@/app/(authed)/o/[org]/settings/actions', () => ({
  inviteMemberAction: vi.fn(),
}))

describe('the invite form', () => {
  it('defaults to member, not to the most powerful role', () => {
    render(<InviteForm slug="acme" />)

    const role = screen.getByLabelText(/role for the invited person/i)
    expect(role).toHaveValue('member')
  })

  it('offers all three roles', () => {
    render(<InviteForm slug="acme" />)

    const role = screen.getByLabelText(/role for the invited person/i)
    const values = Array.from(
      role.querySelectorAll('option'),
      (o) => (o as HTMLOptionElement).value,
    )
    expect(values).toEqual(['owner', 'member', 'viewer'])
  })

  it('asks for an address and offers to send', () => {
    render(<InviteForm slug="acme" />)

    expect(
      screen.getByLabelText(/email address to invite/i),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /send invitation/i }),
    ).toBeInTheDocument()
  })

  it('says nothing before anything has been submitted', () => {
    // The idle state renders no message at all. A form that starts by telling
    // you something went fine, or went wrong, is one people learn to ignore.
    render(<InviteForm slug="acme" />)

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
