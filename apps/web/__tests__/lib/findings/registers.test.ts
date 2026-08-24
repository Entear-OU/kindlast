import { readFileSync } from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

import { awaitingADecision, createsARecord } from '@/lib/findings/registers'

/**
 * Where the Hands is offered, and where it is not (ENT-278).
 *
 * Two things, and the second is the acceptance criterion this issue carries
 * from ENT-261: the explanation has to sit BEFORE the decision panel. That is a
 * property of the page rather than of a component, and the page is an async
 * server component that reads a session, an organisation and a finding, so
 * rendering it here would be a test about three mocks. Reading the source and
 * asserting the order is cruder and it pins the thing that actually matters,
 * which is that a person who scrolls to the decision has already passed what
 * approving would do. The catalogue suite reads Python for the same kind of
 * reason.
 */
const PAGE = path.resolve(
  __dirname,
  '../../../app/(authed)/o/[org]/(needs-profile)/feed/[id]/page.tsx',
)

describe('where the Hands is offered (ENT-278)', () => {
  it('offers it for the action types that create a record', () => {
    expect(createsARecord('create_ropa')).toBe(true)
    expect(createsARecord('create_ai_system')).toBe(true)
    expect(createsARecord('create_dsar')).toBe(true)
  })

  it('does not offer it where approving creates nothing', () => {
    // `review` is every finding whose obligation carries no action type, which
    // is most of them. Approving one records the decision and creates no
    // record, so core-api refuses to explain it, and a control that only ever
    // refused would teach a person the feature is broken.
    expect(createsARecord('review')).toBe(false)
    expect(createsARecord(undefined)).toBe(false)
    expect(createsARecord('')).toBe(false)
  })

  it('does not offer it once the decision has been made', () => {
    // An approval enqueues the execution, and from that instant the payload is
    // the Executor's input rather than a proposal: core-api refuses to rewrite
    // it, which is the guardrail that stops a late run changing what somebody
    // approved.
    expect(awaitingADecision('approved')).toBe(false)
    expect(awaitingADecision('rejected')).toBe(false)
    // Still decidable, so still worth explaining.
    expect(awaitingADecision('pending')).toBe(true)
    expect(awaitingADecision('snoozed')).toBe(true)
  })
})

describe('the finding page (ENT-278)', () => {
  const source = readFileSync(PAGE, 'utf8')

  it('shows what approving will do before the decision panel', () => {
    const explanation = source.indexOf('<ExplainApproval')
    const decision = source.indexOf('<ActControls')

    expect(explanation, 'the page renders the Hands panel').toBeGreaterThan(-1)
    expect(decision, 'the page renders the decision panel').toBeGreaterThan(-1)
    // The whole point of the surface. A person reading down this page meets
    // what approving creates, and what it will leave blank, before they meet
    // the button that does it.
    expect(explanation).toBeLessThan(decision)
  })

  it('keeps the Analyst below the decision, where ENT-270 put it', () => {
    // Asserted here because the two placements look inconsistent until you know
    // why. The chat is something a person chooses to start; this is about the
    // button underneath it. Moving the chat up would put a model's words above
    // the quoted regulation, which is the ENT-164 mistake at page scale.
    expect(source.indexOf('<AskAnalyst')).toBeGreaterThan(
      source.indexOf('<ActControls'),
    )
  })
})
