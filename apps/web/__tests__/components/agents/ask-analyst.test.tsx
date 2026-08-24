import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AskAnalyst } from '@/components/agents/ask-analyst'
import type { AskState } from '@/lib/agents/ask-state'

/**
 * Asking the Analyst about a finding (ENT-270).
 *
 * The rail promised this and three icons did nothing. What matters about the
 * component that replaces them is not that it renders prose: it is that it
 * renders the three outcomes as three different things.
 *
 * A REFUSAL IS NOT AN ERROR, and the whole product's claim is on the line in
 * that distinction. §26.3 makes refusal what a working guardrail produces, so a
 * panel that drew "the model cited an obligation we never showed it" the way it
 * draws "core-api is unreachable" would report the guardrail firing as the
 * guardrail breaking, in front of the one person deciding whether to trust
 * this.
 *
 * The action is a stub here rather than a server action, so these run with no
 * services. What the server action does is the store's and core-api's business,
 * and its own layers are tested where they are.
 */
const RUN = {
  agentRunId: '11111111-1111-4111-8111-111111111111',
  skill: 'analyst.answer',
  skillVersion: '1.0.0',
  model: 'Qwen3.5-2B-Q4_K_M',
  modelVersion: 'aaf42c8b',
  provider: 'instance',
  resolvedCitations: ['gdpr-art-30-ropa'],
}

function answering(state: AskState) {
  return vi.fn(async () => state)
}

function renderPanel(state: AskState) {
  const action = answering(state)
  render(<AskAnalyst slug="acme-ltd" findingId="f-1" action={action} />)
  return action
}

async function ask(question = 'Why does this apply to us?') {
  const user = userEvent.setup()
  await user.type(
    screen.getByLabelText('Ask the Analyst about this finding'),
    question,
  )
  await user.click(screen.getByRole('button', { name: /Ask/i }))
}

describe('asking the Analyst (ENT-270)', () => {
  it('asks nothing until a person asks something', () => {
    const action = renderPanel({ status: 'idle' })
    // No question is sent on render. The panel sits on every finding page, and
    // one that ran a model call on arrival would spend a budget on every person
    // who merely opened a finding.
    expect(action).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /Ask/i })).toBeInTheDocument()
  })

  it('sends the finding and the organisation, and never lets the form name them', () => {
    render(
      <AskAnalyst
        slug="acme-ltd"
        findingId="f-1"
        action={answering(idleState)}
      />,
    )

    // The finding is a hidden field; the ORGANISATION IS NOT, and never is. A
    // hidden org id is a field an attacker can edit, so the action re-resolves
    // it from the slug against the caller's own memberships. The slug travels
    // because the action needs to know which URL it is acting under, and it is
    // resolved rather than trusted.
    const form = screen.getByRole('form', { name: 'Ask the Analyst' })
    expect(within(form).getByDisplayValue('f-1')).toBeInTheDocument()
    expect(within(form).getByDisplayValue('acme-ltd')).toBeInTheDocument()
    expect(form.querySelector('input[name="orgId"]')).toBeNull()
  })

  it('shows the answer and what the person asked', async () => {
    renderPanel({
      status: 'answered',
      question: 'Why does this apply to us?',
      answer:
        'You run payroll in house, so your own staff data is what this is about.',
      run: RUN,
    })
    await ask()

    await waitFor(() => {
      expect(screen.getByText(/You run payroll in house/)).toBeInTheDocument()
    })
    // The textarea is cleared on submit, so a person reading an answer needs
    // the question back beside it or they are reading a reply to nothing.
    expect(screen.getByText(/Why does this apply to us\?/)).toBeInTheDocument()
  })

  it('shows how the answer was produced, which is the record a customer may read', async () => {
    renderPanel({
      status: 'answered',
      question: 'Why us?',
      answer: 'Your payroll is the processing this is about.',
      run: RUN,
    })
    await ask()

    const record = await screen.findByTestId('agent-run')
    // Skill and version together, because that pair is what `agent_runs`
    // stores and a version on its own reproduces nothing.
    expect(record).toHaveTextContent('analyst.answer')
    expect(record).toHaveTextContent('1.0.0')
    // Where it was processed, which is a question any member may ask.
    expect(record).toHaveTextContent(/Qwen3.5-2B-Q4_K_M/)
    // The id, so somebody can ask about this exact run later.
    expect(record).toHaveTextContent(RUN.agentRunId)
    // What it relied on. Never more than the finding's own obligation, because
    // that is the only one the run was offered.
    expect(record).toHaveTextContent('gdpr-art-30-ropa')
  })

  it('draws a refusal as a refusal, not as a fault', async () => {
    renderPanel({
      status: 'refused',
      question: 'What does Article 30 say?',
      reason:
        'the answer stated what the law requires, which is not the Analyst to say',
      run: RUN,
    })
    await ask('What does Article 30 say?')

    const refusal = await screen.findByTestId('answer-refused')
    expect(refusal).toHaveTextContent(/stated what the law requires/)

    // A refused run is still a run, and showing it is what makes the refusal
    // checkable rather than a sentence to take on faith.
    expect(screen.getByTestId('agent-run')).toHaveTextContent(RUN.agentRunId)

    // And no answer. core-api withholds one, and this must not invent a
    // placeholder that reads like one.
    expect(screen.queryByTestId('answer-text')).toBeNull()

    // NOT ANNOUNCED AS AN ERROR, which is the accessible half of the same
    // claim and the half that is easy to lose. `role="alert"` interrupts a
    // screen reader with what it is given, so putting one on this panel would
    // read the guardrail firing out as a fault to the person least able to
    // see that it is not one. The error state below is the only thing here
    // that may carry it.
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('says a deployment has no model rather than blaming the Analyst', async () => {
    renderPanel({ status: 'unavailable' })
    await ask()

    const note = await screen.findByTestId('answer-unavailable')
    expect(note).toHaveTextContent(/no model/i)
    // Not a refusal. Nothing ran, so there is nothing to show a record of, and
    // drawing one would claim a run that never happened.
    expect(screen.queryByTestId('agent-run')).toBeNull()
    expect(screen.queryByTestId('answer-refused')).toBeNull()
  })

  it('shows an error as an error, with no run behind it', async () => {
    renderPanel({ status: 'error', message: 'core-api is unreachable.' })
    await ask()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'core-api is unreachable.',
    )
    expect(screen.queryByTestId('agent-run')).toBeNull()
  })

  it('tells a person the Analyst will not state the law, before they ask', () => {
    render(
      <AskAnalyst
        slug="acme-ltd"
        findingId="f-1"
        action={answering(idleState)}
      />,
    )
    // The most natural question about a finding is "what does the article
    // say", and it is the one question this refuses. Saying so up front is the
    // difference between a bounded assistant and one that seems broken.
    expect(
      screen.getByText(/will not tell you what the law says/i),
    ).toBeInTheDocument()
  })

  it('writes no em dashes or en dashes in anything a person reads', () => {
    const { container } = render(
      <AskAnalyst
        slug="acme-ltd"
        findingId="f-1"
        action={answering(idleState)}
      />,
    )
    expect(container.textContent).not.toContain('—')
    expect(container.textContent).not.toContain('–')
  })
})

const idleState: AskState = { status: 'idle' }
