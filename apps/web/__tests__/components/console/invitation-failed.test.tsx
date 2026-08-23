import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { InvitationFailed } from '@/components/console/invitation-failed'

/**
 * The sentence a failed invitation never used to get (ENT-267).
 *
 * `/invite/{token}` has always redirected a failed redemption to
 * `/workspace?error=invitation`, and nothing read the parameter. The person
 * landed in an organisation of their own with no indication that anything had
 * been attempted, which since PR #227 is the ordinary outcome rather than a
 * rare one: an invitation followed by anybody except the person it names is
 * refused by design, and clicking your own invitation link to see what the
 * recipient will see is exactly that.
 *
 * Two properties, and the second is the one with teeth.
 */
describe('a failed invitation', () => {
  it('names the account the invitation was tried with', () => {
    // Without this it is a dead end. The commonest cause is being signed in as
    // the wrong person, and somebody can only act on that if they are told
    // which person the browser thinks they are.
    render(<InvitationFailed email="ada@example.test" />)

    expect(screen.getByTestId('invitation-failed')).toHaveTextContent(
      'ada@example.test',
    )
  })

  /**
   * The property, not a wording preference.
   *
   * core-api answers expired, already redeemed, never real and addressed to
   * somebody else identically, so that holding a session cannot be used to
   * discover which invitations exist. A message that named the cause would
   * hand back exactly the distinction the API refuses to make, and it would do
   * it in the one place a person is guaranteed to be looking.
   */
  it('says nothing that distinguishes why it failed', () => {
    render(<InvitationFailed email="ada@example.test" continueTo="/o/acme" />)

    const message = screen.getByTestId('invitation-failed')
    for (const oracle of [
      /expired/i,
      /already/i,
      /no longer/i,
      /revoked/i,
      /does not exist/i,
      /not found/i,
      /unknown/i,
      /invalid/i,
      /someone else/i,
      /somebody else/i,
      /addressed to/i,
      /was sent to/i,
    ]) {
      expect(message).not.toHaveTextContent(oracle)
    }
  })

  it('offers the way onward when there is an organisation to go to', () => {
    render(<InvitationFailed email="ada@example.test" continueTo="/o/acme" />)

    expect(screen.getByRole('link', { name: /continue/i })).toHaveAttribute(
      'href',
      '/o/acme',
    )
  })

  // Somebody holding a session with no membership at all is reachable:
  // provisioning is idempotent but it can fail. Inventing a link for them
  // would produce a URL that 404s and reads as data loss rather than as the
  // absence of an organisation.
  it('offers no link when there is nowhere to go', () => {
    render(<InvitationFailed email="ada@example.test" />)

    expect(screen.queryByRole('link')).toBeNull()
  })

  // The account is what makes the message actionable, but not knowing it is
  // no reason to say nothing: GetCurrentUser can fail, and the invitation
  // still did not work.
  it('still explains itself when the account cannot be named', () => {
    render(<InvitationFailed />)

    expect(screen.getByTestId('invitation-failed')).toHaveTextContent(
      /invitation could not be used/i,
    )
  })
})
