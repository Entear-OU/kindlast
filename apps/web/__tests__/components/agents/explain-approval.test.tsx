import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ExplainApproval } from '@/components/agents/explain-approval'
import type { ExplainState } from '@/lib/agents/explain-state'

/**
 * What approving will do, from the Hands (ENT-278).
 *
 * THIS IS THE MOST CONSEQUENTIAL PLACE IN THE PRODUCT TO PUT GENERATED PROSE.
 * It sits above the decision panel, on the page where somebody decides whether
 * to write something into their own compliance record. So what these assert is
 * not that it renders a paragraph: it is that the paragraph is marked as the
 * Hands', that the plan says what it could not fill as loudly as what it could,
 * and that a refusal is drawn as the guardrail working rather than as a fault.
 *
 * The action is a stub rather than a server action, so these run with no
 * services. What the server action does is core-api's business and is tested
 * where it lives.
 */
const RUN = '11111111-1111-4111-8111-111111111111'

const EXPLAINED: ExplainState = {
  status: 'explained',
  registerLabel: 'your Article 30 record of processing activities',
  explanation:
    'Approving this adds one entry covering the payroll you run in house, ' +
    'with the lawful basis you already recorded for it.',
  prepared: [
    {
      name: 'legal_basis',
      label: 'the lawful basis you rely on',
      values: ['legal obligation'],
      fromFact: 'payroll.legal_basis',
    },
  ],
  leftForYou: [
    {
      name: 'retention_period',
      label: 'how long you keep it',
      why: 'you have not told us how long you keep payroll records',
    },
  ],
  agentRunId: RUN,
}

function answering(state: ExplainState) {
  return vi.fn(async () => state)
}

function renderPanel(state: ExplainState) {
  const action = answering(state)
  render(<ExplainApproval slug="acme-ltd" findingId="f-1" action={action} />)
  return action
}

async function askTheHands() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Ask the Hands/i }))
}

describe('what approving will do (ENT-278)', () => {
  it('runs nothing until a person asks for it', () => {
    const action = renderPanel({ status: 'idle' })
    // A run spends a model budget AND writes a proposed payload onto the
    // finding. A panel that ran on arrival would do both to every person who
    // merely opened a finding to read it, which is worse than the same mistake
    // on the Analyst's box: this one changes what approving would create.
    expect(action).not.toHaveBeenCalled()
    expect(
      screen.getByRole('button', { name: /Ask the Hands/i }),
    ).toBeInTheDocument()
  })

  it('sends the finding and the slug, and never lets the form name the organisation', () => {
    render(
      <ExplainApproval
        slug="acme-ltd"
        findingId="f-1"
        action={answering({ status: 'idle' })}
      />,
    )
    // The same rule the act path and the Analyst's box follow: a hidden org id
    // is a field somebody can edit, so the action re-resolves the organisation
    // from the slug against the caller's own memberships.
    const form = screen.getByRole('form', { name: 'Ask the Hands' })
    expect(within(form).getByDisplayValue('f-1')).toBeInTheDocument()
    expect(within(form).getByDisplayValue('acme-ltd')).toBeInTheDocument()
    expect(form.querySelector('input[name="orgId"]')).toBeNull()
  })

  it('marks the explanation as the Hands, not as the product speaking', async () => {
    renderPanel(EXPLAINED)
    await askTheHands()

    const explanation = await screen.findByTestId('approval-explanation')
    expect(explanation).toHaveTextContent(/payroll you run in house/)

    // GENERATED PROSE IS NEVER UNMARKED. The same sentence the Analyst's
    // narrative carries, in the same words, because a second phrasing for the
    // same claim is a second thing to keep true.
    const attribution = screen.getByTestId('approval-attribution')
    expect(attribution).toHaveTextContent(/Prepared by the Hands/i)
    expect(attribution).toHaveTextContent(/not a statement of the law/i)
    // The run, so somebody can ask about this exact one later.
    expect(attribution).toHaveTextContent(RUN)
  })

  it('says which register in words core-api wrote rather than the model', async () => {
    renderPanel(EXPLAINED)
    await askTheHands()

    const register = await screen.findByTestId('approval-register')
    expect(register).toHaveTextContent(
      /your Article 30 record of processing activities/,
    )
    // Outside the attributed paragraph, deliberately. What approving does is a
    // statement about this product, the model is not the authority on it, and a
    // reader is entitled to know which sentence came from where.
    expect(
      within(screen.getByTestId('approval-explanation')).queryByTestId(
        'approval-register',
      ),
    ).toBeNull()
  })

  it('shows what it left for a person as loudly as what it filled', async () => {
    renderPanel(EXPLAINED)
    await askTheHands()

    const filled = await screen.findByTestId('approval-prepared')
    // The column in a person's words, the value, and the fact it came from. A
    // value with no source is a fabrication a customer cannot detect.
    expect(filled).toHaveTextContent('the lawful basis you rely on')
    expect(filled).toHaveTextContent('legal obligation')
    expect(filled).toHaveTextContent('payroll.legal_basis')

    // AND THE OTHER HALF. A plan listing three filled columns and saying
    // nothing about the fourth reads as complete, which is the failure this
    // agent exists to fix.
    const left = screen.getByTestId('approval-left')
    expect(left).toHaveTextContent('how long you keep it')
    expect(left).toHaveTextContent(/have not told us how long/)
  })

  it('says so plainly when a run filled nothing at all', async () => {
    renderPanel({
      ...EXPLAINED,
      prepared: [],
      leftForYou: [
        {
          name: 'purpose',
          label: 'why you process this data',
          why: 'you have not told us what payroll is for',
        },
      ],
    })
    await askTheHands()

    // An empty "filled" list drawn as an empty box would read as a defect. A
    // run that could fill nothing is an ordinary outcome for an organisation
    // that has recorded little about itself, and it is worth saying in words.
    expect(
      await screen.findByTestId('approval-nothing-filled'),
    ).toHaveTextContent(/nothing/i)
    expect(screen.queryByTestId('approval-prepared')).toBeNull()
    expect(screen.getByTestId('approval-left')).toHaveTextContent(
      'why you process this data',
    )
  })

  it('draws a refusal as a refusal, not as a fault', async () => {
    renderPanel({
      status: 'refused',
      reason: 'the run asked for a tool it was not offered',
      agentRunId: RUN,
    })
    await askTheHands()

    const refusal = await screen.findByTestId('approval-refused')
    expect(refusal).toHaveTextContent(/tool it was not offered/)
    // A refused run is still a run, and showing it is what makes the refusal
    // checkable rather than a sentence to take on faith.
    expect(refusal).toHaveTextContent(RUN)
    // No explanation. core-api withholds a refused one, and this must not
    // invent a placeholder that reads like one.
    expect(screen.queryByTestId('approval-explanation')).toBeNull()
    // NOT ANNOUNCED AS AN ERROR. `role="alert"` interrupts a screen reader
    // with what it is given, so putting one here would read the guardrail
    // firing out as a fault to the person least able to see that it is not.
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('says a deployment runs no model rather than blaming the Hands', async () => {
    renderPanel({ status: 'unavailable' })
    await askTheHands()

    const note = await screen.findByTestId('approval-unavailable')
    expect(note).toHaveTextContent(/no model/i)
    // Nothing ran, so there is no run to show, and drawing one would claim a
    // record that does not exist.
    expect(note).not.toHaveTextContent(RUN)
    expect(screen.queryByTestId('approval-refused')).toBeNull()
  })

  it('shows an error as an error', async () => {
    renderPanel({ status: 'error', message: 'core-api is unreachable.' })
    await askTheHands()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'core-api is unreachable.',
    )
    expect(screen.queryByTestId('approval-explanation')).toBeNull()
  })

  it('says the decision stays with the reader, before they ask for anything', () => {
    render(
      <ExplainApproval
        slug="acme-ltd"
        findingId="f-1"
        action={answering({ status: 'idle' })}
      />,
    )
    // The panel sits directly above the approve button. Somebody reading a
    // machine's paragraph there is entitled to know, without pressing anything,
    // that nothing here can decide for them.
    expect(screen.getByText(/never decides/i)).toBeInTheDocument()
  })

  it('writes no em dashes or en dashes in anything a person reads', async () => {
    const { container } = render(
      <ExplainApproval
        slug="acme-ltd"
        findingId="f-1"
        action={answering(EXPLAINED)}
      />,
    )
    await askTheHands()
    await waitFor(() => {
      expect(screen.getByTestId('approval-explanation')).toBeInTheDocument()
    })

    expect(container.textContent).not.toContain('—')
    expect(container.textContent).not.toContain('–')
  })
})
