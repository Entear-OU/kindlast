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
import { KINDY_IDLE, type KindyState } from '@/components/console/kindy-state'

// The composer's server action, stubbed: what it does when called is the
// composer's own test's business (kindy-composer.test.tsx). Here it only has
// to exist, the way the layout always provides it in production.
const noopKindy = async (): Promise<KindyState> => KINDY_IDLE

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
    render(<AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} />)

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
    render(
      <AgentRail
        orgSlug="acme-ltd"
        kindyAction={noopKindy}
        activity={activity}
      />,
    )

    for (const item of activity) {
      expect(
        screen.getByRole('link', { name: new RegExp(item.title.slice(0, 20)) }),
      ).toHaveAttribute('href', `/o/acme-ltd/feed/${item.id}`)
    }
  })

  it('says nothing has landed rather than nothing happened, when there is nothing to list', () => {
    render(
      <AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} activity={[]} />,
    )
    // Absent data and empty data read the same here on purpose: the rail is
    // chrome on every page and the feed is where an empty list is a claim.
    expect(screen.getByText(/Nothing yet/)).toBeVisible()
  })

  it('keeps the two layouts apart in the DOM', () => {
    const { container } = render(
      <>
        <AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} />
        <AgentRail
          orgSlug="acme-ltd"
          kindyAction={noopKindy}
          variant="mobile"
        />
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
    it('carries the composer, wired to the action the layout injects', () => {
      render(<AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} />)

      // Presence and wiring only: what an exchange renders is the composer's
      // own test's business. The first cut of this box navigated to the feed,
      // and a person who typed "hello" into a face's message box and landed
      // on a list page rightly reported it broken, so the box answering in
      // place IS the contract now.
      const input = screen.getByRole('textbox', { name: /Message Kindy/ })
      expect(input).toHaveAttribute('name', 'ask')
      expect(
        screen.getByRole('button', { name: /Send to Kindy/ }),
      ).toBeVisible()
    })

    it('renders call and walkthrough as disabled controls that say so', () => {
      const { container } = render(
        <AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} />,
      )

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
      render(<AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} />)
      // The sentence this replaced described writing, speech and video as one
      // step away. One of the three arrived and the other two are not close, so
      // repeating it would be the placeholder reading as a feature again.
      expect(screen.queryByText(/Talking to them is coming/)).toBeNull()
    })
  })

  /**
   * The presence dot (ENT-296).
   *
   * It was `bg-emerald-500`, hardcoded, with a comment saying it should go
   * grey the day Kindy stops answering rather than the day somebody
   * remembers. Nothing existed for it to read, so it was a live-looking
   * indicator with nothing behind it: the ENT-202 shape, on the one control
   * whose entire job is to say whether something is live.
   *
   * These pin the three sentences it has to be able to say, and the rule that
   * makes it legible to somebody who cannot see the colour.
   */
  describe('the presence dot', () => {
    it('says Kindy is answering when Intelligence is reachable', () => {
      render(
        <AgentRail
          orgSlug="acme-ltd"
          kindyAction={noopKindy}
          status={{ availability: 'AVAILABILITY_REACHABLE' }}
        />,
      )

      expect(
        screen.getByRole('status', { name: /Kindy is answering/i }),
      ).toBeInTheDocument()
    })

    it('says Kindy is not answering when Intelligence has stopped', () => {
      // THE BUG THE WHOLE CHANGE EXISTS FOR. This deployment used to draw a
      // green dot, because the only signal was that a URL was configured.
      render(
        <AgentRail
          orgSlug="acme-ltd"
          kindyAction={noopKindy}
          status={{ availability: 'AVAILABILITY_UNREACHABLE' }}
        />,
      )

      const dot = screen.getByRole('status', { name: /not answering/i })
      expect(dot).toBeInTheDocument()
      expect(dot.className).not.toMatch(/emerald/)
    })

    it('distinguishes a deployment that runs no model from one that is broken', () => {
      // Supported rather than broken. A self-hoster who never enabled the
      // model profile has nothing to fix and must not be told they do.
      render(
        <AgentRail
          orgSlug="acme-ltd"
          kindyAction={noopKindy}
          status={{ availability: 'AVAILABILITY_NOT_CONFIGURED' }}
        />,
      )

      const dot = screen.getByRole('status')
      expect(dot).toHaveAccessibleName(/no model/i)
      // And specifically NOT the outage wording, which is the whole point of
      // keeping the two states apart.
      expect(dot).not.toHaveAccessibleName(/not answering/i)
    })

    it('claims nothing when the status could not be read', () => {
      // An unreadable status is not a claim that Kindy is up. The rail is
      // chrome on every page and this read can fail on its own, so absent has
      // to be its own state rather than defaulting to green.
      render(<AgentRail orgSlug="acme-ltd" kindyAction={noopKindy} />)

      const dot = screen.getByRole('status')
      expect(dot.className).not.toMatch(/emerald/)
      expect(dot).not.toHaveAccessibleName(/is answering/i)
    })

    it('never says it in colour alone', () => {
      // The rule the severity dots already follow, and it matters more here:
      // this dot is 12 pixels and carries the only presence signal on the
      // card, so a reader who cannot distinguish the colours would otherwise
      // have nothing at all.
      for (const availability of [
        'AVAILABILITY_REACHABLE',
        'AVAILABILITY_UNREACHABLE',
        'AVAILABILITY_NOT_CONFIGURED',
      ] as const) {
        const { unmount } = render(
          <AgentRail
            orgSlug="acme-ltd"
            kindyAction={noopKindy}
            status={{ availability }}
          />,
        )
        expect(screen.getByRole('status')).toHaveAccessibleName(/\S/)
        unmount()
      }
    })
  })
})
