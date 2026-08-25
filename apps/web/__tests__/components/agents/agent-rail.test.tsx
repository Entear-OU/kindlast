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

import { AgentRail } from '@/components/console/agent-rail'
import { AGENTS, STATUS_LABEL } from '@/lib/agents/catalog'

/**
 * The agent rail (ENT-222, made per-agent and addressable by ENT-232).
 *
 * The rail used to say "Not scheduled yet" under all four, which was true when
 * it was written and stopped being true when ENT-218 shipped the Analyst. A
 * blanket claim about four things is wrong the moment one of them changes, and
 * a rail that is wrong in the reassuring direction is the ENT-161 failure
 * running the other way: understating is safer than overstating, and neither
 * is what a customer asked for.
 *
 * Rendered directly rather than through ConsoleShell, because the rail now
 * takes the organisation and the shell test is about the chrome around it.
 */
describe('the agent rail (ENT-232)', () => {
  it('makes every agent addressable by name', () => {
    render(<AgentRail orgSlug="acme-ltd" />)

    for (const agent of AGENTS) {
      const link = screen.getByRole('link', { name: new RegExp(agent.name) })
      expect(link).toHaveAttribute('href', `/o/acme-ltd/agents/${agent.slug}`)
    }
  })

  it('reaches the four as a set, not only one at a time', () => {
    render(<AgentRail orgSlug="acme-ltd" />)
    expect(
      screen.getByRole('link', { name: 'What each one is allowed to do' }),
    ).toHaveAttribute('href', '/o/acme-ltd/agents')
  })

  it('gives each agent its own status rather than one claim for all four', () => {
    const { container } = render(<AgentRail orgSlug="acme-ltd" />)

    // Scoped to the pipeline, because the card at the foot carries status lines
    // of its own since ENT-270 and those are about chat, call and walkthrough
    // rather than about an agent. Counting both would tie this assertion to a
    // number that moves whenever either half does.
    const pipeline = container.querySelector('ol')
    expect(pipeline).not.toBeNull()
    const list = within(pipeline!)

    // All four are working as of ENT-280, and the count arrived one commit at
    // a time: the Watcher and the Analyst at ENT-258, the Hands when its
    // surface landed at ENT-278, the Messenger when the doorbell workflow
    // started running its draft at ENT-280. Saying the same thing about all
    // four is the failure this replaced, so the count is asserted rather than
    // the labels merely being present.
    expect(list.getAllByText(STATUS_LABEL['working'])).toHaveLength(4)
    // Both zero, and asserted rather than dropped: an absence is a claim, and
    // these are the ones that would go quietly wrong the day a fifth agent
    // joins the rail ahead of its skill, or an agent's half gets rebuilt. The
    // states stay rendered; partly-working has been reached for twice
    // (ENT-261's Hands, ENT-260's Messenger) and earned its keep both times.
    expect(list.queryAllByText(STATUS_LABEL['not-built'])).toHaveLength(0)
    expect(list.queryAllByText(STATUS_LABEL['partly-working'])).toHaveLength(0)
  })

  it('no longer claims that nothing is scheduled', () => {
    render(<AgentRail orgSlug="acme-ltd" />)
    expect(screen.queryByText('Not scheduled yet')).toBeNull()
  })

  it('keeps the two layouts apart in the DOM', () => {
    // Both render at once in jsdom (no media queries), and two elements with
    // one id is invalid HTML that gives a screen reader two things to land on.
    const { container } = render(
      <>
        <AgentRail orgSlug="acme-ltd" />
        <AgentRail orgSlug="acme-ltd" variant="mobile" />
      </>,
    )
    const ids = [...container.querySelectorAll('[id]')].map((el) => el.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  /**
   * The card at the foot of the rail (ENT-270).
   *
   * It used to say "talking to them is coming" over three icons, and the three
   * were drawn as text rather than controls precisely because none of them did
   * anything (ENT-202: a control that silently does nothing is worse than one
   * visibly absent). One of the three is now real, and the card has to stop
   * describing all three the same way, which is the same failure the rail's
   * per-agent status was written to fix one level up.
   */
  describe('talking to them (ENT-270)', () => {
    it('offers chat as a link, because it exists now', () => {
      render(<AgentRail orgSlug="acme-ltd" />)

      // Into the feed rather than into a chat window. The Analyst answers about
      // one finding, so a conversation has to start at a finding: a chat with
      // no subject would have no obligation to check a citation against, which
      // is the guardrail the whole feature rests on.
      expect(screen.getByRole('link', { name: /Chat/ })).toHaveAttribute(
        'href',
        '/o/acme-ltd/feed',
      )
    })

    it('says call and walkthrough are not built, in the words the agents page uses', () => {
      const { container } = render(<AgentRail orgSlug="acme-ltd" />)

      // The same vocabulary as the Messenger and the Hands, deliberately. A
      // second phrase for the same state is a second thing to keep true, and
      // ENT-232 already paid for that lesson one level up this component.
      for (const label of ['Call', 'Walkthrough']) {
        const row = screen.getByText(label).closest('li')
        expect(row).not.toBeNull()
        expect(within(row!).getByText(STATUS_LABEL['not-built'])).toBeVisible()
      }

      // Still not controls. Nothing behind either of them, so a person who
      // pressed one would wait for an answer that is not coming.
      for (const label of ['Call', 'Walkthrough']) {
        expect(screen.queryByRole('button', { name: label })).toBeNull()
        expect(
          within(container).queryByRole('link', { name: label }),
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
