import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'

/**
 * The state every authenticated page used to render as nothing at all.
 *
 * Four pages returned `null` when `resolveOrg` reported `unavailable`, which
 * puts the console shell around an empty column. Met in a browser after a
 * session's token expired, and indistinguishable from the product being broken.
 */
describe('a workspace that could not be loaded', () => {
  it('keeps the surface its own heading rather than going generic', () => {
    render(<WorkspaceUnavailable title="Feed" />)

    expect(screen.getByRole('heading', { name: 'Feed' })).toBeInTheDocument()
  })

  // The decision worth protecting. `unavailable` is both an expired token and
  // core-api being down, and this cannot tell them apart, so it must not assert
  // either. Claiming "your session expired" would be a guess presented as fact.
  it('names both possibilities rather than guessing which', () => {
    render(<WorkspaceUnavailable title="Overview" />)

    const message = screen.getByTestId('workspace-unavailable')
    expect(message).toHaveTextContent(/session may have expired/i)
    expect(message).toHaveTextContent(/usually temporary/i)
  })

  // An empty register is a claim about the compliance record. This is a claim
  // about the connection, and confusing the two tells a customer their record
  // is empty when nobody looked.
  it('does not read as an empty workspace', () => {
    render(<WorkspaceUnavailable title="Compliance record" />)

    const message = screen.getByTestId('workspace-unavailable')
    expect(message).not.toHaveTextContent(/nothing on file/i)
    expect(message).not.toHaveTextContent(/no findings/i)
  })
})
