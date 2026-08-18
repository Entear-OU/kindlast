import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AgentProfile } from '@/components/agents/agent-profile'
import { agentBySlug } from '@/lib/agents/catalog'

/**
 * One agent's page (ENT-232).
 *
 * "Addressable by name in the rail" is only worth anything if the thing behind
 * the name answers the two questions a person actually has: is it running, and
 * what is it allowed to touch. Both are refusable claims here rather than
 * marketing: the tool list comes from the skill's own allow-list, and an agent
 * with no skill shows no list at all.
 *
 * A plain component taking the agent as a prop, for the same reason the shell
 * is one: the page above it is an async server component that resolves a
 * session and a slug, and folding the copy into it would mean rendering React
 * to test a tenancy decision.
 */
describe('an agent profile (ENT-232)', () => {
  it('shows the skill and its version, because a run records both', () => {
    const analyst = agentBySlug('analyst')!
    render(<AgentProfile agent={analyst} />)

    expect(screen.getByText(analyst.skill!.name)).toBeInTheDocument()
    expect(
      screen.getByText(new RegExp(analyst.skill!.version)),
    ).toBeInTheDocument()
  })

  it('says the Analyst may call no tools, rather than saying nothing', () => {
    // An empty allow-list is a statement: this skill is given what it needs and
    // then answers. Rendering nothing where the list would be reads as "we did
    // not check", which is the opposite claim.
    render(<AgentProfile agent={agentBySlug('analyst')!} />)
    expect(screen.getByTestId('tool-allow-list')).toHaveTextContent(/no tools/i)
  })

  it('shows no tool list for an agent that has no skill', () => {
    // The Messenger's allow-list is not empty, it is absent. Showing an empty
    // one would claim a guardrail with nothing behind it.
    render(<AgentProfile agent={agentBySlug('messenger')!} />)
    expect(screen.queryByTestId('tool-allow-list')).toBeNull()
  })

  it('says what remains, for an agent that is not finished', () => {
    const messenger = agentBySlug('messenger')!
    render(<AgentProfile agent={messenger} />)
    expect(screen.getByText(messenger.remaining!)).toBeInTheDocument()
  })

  it('says when it runs and what it may change, for every agent', () => {
    for (const slug of ['watcher', 'analyst', 'messenger', 'hands']) {
      const agent = agentBySlug(slug)!
      const { unmount } = render(<AgentProfile agent={agent} />)
      expect(screen.getByText(agent.runs)).toBeInTheDocument()
      expect(screen.getByText(agent.effects)).toBeInTheDocument()
      unmount()
    }
  })
})
