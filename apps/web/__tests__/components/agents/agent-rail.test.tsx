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
    render(<AgentRail orgSlug="acme-ltd" />)

    // The Watcher and the Analyst are skills on the harness; the Messenger and
    // the Hands do not exist. Saying the same thing about all four is the
    // failure this replaced, so the counts are asserted rather than the labels
    // merely being present.
    expect(screen.getAllByText(STATUS_LABEL['working'])).toHaveLength(2)
    expect(screen.getAllByText(STATUS_LABEL['not-built'])).toHaveLength(2)
    // `queryAllByText`, because nothing is partly working since ENT-258 and
    // `getAllByText` throws on none. The state is still rendered for the next
    // agent that gets half built.
    expect(screen.queryAllByText(STATUS_LABEL['partly-working'])).toHaveLength(
      0,
    )
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

  it('does not render call, chat or video as controls', () => {
    // Announced, not offered (ENT-202). There is no conversational agent
    // behind any of them, and a person who pressed one would wait for an
    // answer that is not coming.
    const { container } = render(<AgentRail orgSlug="acme-ltd" />)
    for (const label of ['Chat', 'Call', 'Walkthrough']) {
      expect(screen.getByText(label)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: label })).toBeNull()
      expect(within(container).queryByRole('link', { name: label })).toBeNull()
    }
  })
})
