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
  it('shows every skill and its version, because a run records both', () => {
    // EVERY skill, not the first. The Analyst has two since ENT-270 and they
    // carry different versions, so showing one would put a version on this page
    // that no run of the other ever recorded. Walked from the catalogue rather
    // than named here, so a third skill is covered the day it is added.
    const analyst = agentBySlug('analyst')!
    render(<AgentProfile agent={analyst} />)

    expect(analyst.skills!.length).toBeGreaterThan(1)
    for (const skill of analyst.skills!) {
      expect(screen.getByText(skill.name)).toBeInTheDocument()
      expect(
        screen.getAllByText(new RegExp(skill.version)).length,
      ).toBeGreaterThan(0)
    }
  })

  it('says the Analyst may call no tools, rather than saying nothing', () => {
    // An empty allow-list is a statement: this skill is given what it needs and
    // then answers. Rendering nothing where the list would be reads as "we did
    // not check", which is the opposite claim. One list per skill, because the
    // claim is about a skill and not about an agent.
    const analyst = agentBySlug('analyst')!
    render(<AgentProfile agent={analyst} />)

    const lists = screen.getAllByTestId('tool-allow-list')
    expect(lists).toHaveLength(analyst.skills!.length)
    for (const list of lists) {
      expect(list).toHaveTextContent(/no tools/i)
    }
  })

  it('shows no tool list for an agent that has no skill', () => {
    // An absent allow-list is not an empty one. Showing an empty list where
    // there is no skill would claim a guardrail with nothing behind it, which
    // is the opposite claim from the Analyst's deliberate emptiness above.
    //
    // THE MESSENGER USED TO BE THIS TEST'S SUBJECT, AND ENT-260 TOOK IT AWAY.
    // Every agent in the catalogue has a skill now, so the case is built here
    // rather than found: the branch is still in the component and is what the
    // next agent added to the rail ahead of its skill will land on, and
    // deleting the test because nothing currently exercises it is how that
    // branch stops being covered right before it is needed.
    render(
      <AgentProfile
        agent={{ ...agentBySlug('messenger')!, skills: undefined }}
      />,
    )
    expect(screen.queryByTestId('tool-allow-list')).toBeNull()
  })

  it('names the one tool the Messenger may call, because that is the claim', () => {
    // The whole of ENT-260 is that it drafts and sends only through the
    // dispatch path, and this list is where a customer checks that rather than
    // taking our word for it. One entry, and it is the one that hands a draft
    // over: nothing here sends, names a recipient or chooses a channel.
    const messenger = agentBySlug('messenger')!
    render(<AgentProfile agent={messenger} />)

    const list = screen.getByTestId('tool-allow-list')
    expect(list).toHaveTextContent('queue_message')
    for (const forbidden of ['send_email', 'send_telegram', 'deliver']) {
      expect(list).not.toHaveTextContent(forbidden)
    }
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
