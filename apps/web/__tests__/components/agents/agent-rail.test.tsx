import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// `next/link` resolves to a plain anchor in the test env (no Next runtime).
vi.mock('next/link', () => ({
  default: ({
    href,
    children,
    ...rest
  }: {
    href: string
    children: React.ReactNode
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}))

import { AgentRail, type ActivityItem } from '@/components/console/agent-rail'

/**
 * Kindy's panel (ENT-222, ENT-232, ENT-270, reshaped into a contact card).
 *
 * The rail used to be a directory of the four agents, and its tests pinned
 * that directory: every agent a link, every status counted. The panel is
 * Kindy now and the agents live behind its "more" button on the agents page,
 * so what these tests pin moved with the design. What did not move is the
 * discipline underneath: nothing on the card is a live-looking control with
 * nothing behind it (ENT-202), the one path to the agents page stays a real
 * link (losing it orphans that surface, the ENT-245 failure shape), and an
 * absent read renders as nothing-listed rather than as a claim.
 *
 * Rendered directly rather than through ConsoleShell, because the rail takes
 * its own props and the shell test is about the chrome around it.
 */
describe("Kindy's panel", () => {
  it('keeps the one path to the agents page', () => {
    render(<AgentRail orgSlug="acme-ltd" />)

    // The kebab on the contact card. This is the console's only route to the
    // page saying what each agent is allowed to do, so it is asserted as a
    // link with a real destination rather than trusted as decoration.
    expect(
      screen.getByRole('link', { name: /About Kindy's agents/ }),
    ).toHaveAttribute('href', '/o/acme-ltd/agents')
  })

  it('lists activity newest-in, each row a door to its finding', () => {
    const activity: ActivityItem[] = [
      {
        id: 'f-1',
        title: 'Profile gap: Records of Processing Activities (ROPA)',
        severity: 'high',
        at: new Date().toISOString(),
      },
      {
        id: 'f-2',
        title: 'Profile gap: AI literacy',
        severity: 'medium',
        at: new Date().toISOString(),
      },
    ]
    render(<AgentRail orgSlug="acme-ltd" activity={activity} />)

    for (const item of activity) {
      expect(
        screen.getByRole('link', { name: new RegExp(item.title.slice(0, 20)) }),
      ).toHaveAttribute('href', `/o/acme-ltd/feed/${item.id}`)
    }
  })

  it('says nothing has landed rather than nothing happened, when there is nothing to list', () => {
    render(<AgentRail orgSlug="acme-ltd" activity={[]} />)
    // Absent data and empty data read the same here on purpose: the rail is
    // chrome on every page and the feed is where an empty list is a claim.
    expect(screen.getByText(/Nothing yet/)).toBeVisible()
  })

  it('keeps the two layouts apart in the DOM', () => {
    const { container } = render(
      <>
        <AgentRail orgSlug="acme-ltd" />
        <AgentRail orgSlug="acme-ltd" variant="mobile" />
      </>,
    )
    // Two elements with one id is invalid HTML, and the phone's tab bar links
    // to the mobile rail by id, so the ids have to differ per variant.
    const ids = [...container.querySelectorAll('[id]')].map((el) => el.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  /**
   * Kindy's contact card and composer (ENT-270, reshaped with the card).
   */
  describe("Kindy's card and composer (ENT-270)", () => {
    it('carries a composer that is a real form aimed at the feed, not a prop', () => {
      render(<AgentRail orgSlug="acme-ltd" />)

      // A plain GET form: the feed receives `ask` and relays it to the Ask
      // box on the newest open finding. If either half of that path breaks,
      // this input becomes the silently-dead control ENT-202 forbids, so the
      // form's target and the field's name are the contract asserted here.
      const input = screen.getByRole('textbox', { name: /Message Kindy/ })
      expect(input).toHaveAttribute('name', 'ask')
      expect(input.closest('form')).toHaveAttribute(
        'action',
        '/o/acme-ltd/feed',
      )
      expect(
        screen.getByRole('button', { name: /Send to Kindy/ }),
      ).toBeVisible()
    })

    it('renders call and walkthrough as disabled controls that say so', () => {
      const { container } = render(<AgentRail orgSlug="acme-ltd" />)

      // Disabled and labelled, rather than absent and rather than live: the
      // reference design carries the buttons, and the honest version of a
      // button with nothing behind it is one that visibly cannot be pressed.
      for (const label of ['Call', 'Walkthrough']) {
        const control = screen.getByRole('button', {
          name: new RegExp(`${label} \\(not built yet\\)`),
        })
        expect(control).toBeDisabled()
        // And never a link: a disabled button cannot navigate, a link always
        // can, and these two must not go anywhere.
        expect(
          within(container).queryByRole('link', { name: new RegExp(label) }),
        ).toBeNull()
      }
    })

    it('no longer promises that all three are coming', () => {
      render(<AgentRail orgSlug="acme-ltd" />)
      // The sentence this replaced described writing, speech and video as one
      // step away. One of the three arrived and the other two are not close, so
      // repeating it would be the placeholder reading as a feature again.
      expect(screen.queryByText(/Talking to them is coming/)).toBeNull()
    })
  })
})
