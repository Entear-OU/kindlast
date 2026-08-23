/**
 * What the console says after an approval (ENT-271).
 *
 * The Executor became a workflow, so approving a finding that creates a record
 * no longer creates it inside the same transaction: the approval is durable at
 * once and the record follows a second or two later. "Approved." alone would
 * send somebody to Records, find nothing there yet, and read a working system
 * as a broken one, which is the state ENT-257 asked to be named rather than
 * left as a gap.
 *
 * The action type reaches the server action through a hidden field, so what is
 * tested here is that the field carries it and that a finding which creates
 * nothing does not promise a record.
 */
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import { ActControls } from '@/components/feed/act-controls'
import { idle, type FindingActionState } from '@/lib/findings/action-state'

const noop = async (): Promise<FindingActionState> => idle

const actions = { approve: noop, reject: noop, snooze: noop }

describe('the approve form', () => {
  it('carries what approving will create, so the confirmation can say it', () => {
    const { container } = render(
      <ActControls
        slug="acme"
        findingId="f1"
        status="pending"
        actionType="create_ropa"
        actions={actions}
      />,
    )

    const field = container.querySelector('input[name="actionType"]')
    expect(field).not.toBeNull()
    expect(field).toHaveValue('create_ropa')
  })

  it('sends an empty action type for a finding that creates nothing', () => {
    const { container } = render(
      <ActControls
        slug="acme"
        findingId="f1"
        status="pending"
        actions={actions}
      />,
    )

    expect(container.querySelector('input[name="actionType"]')).toHaveValue('')
    // And the form is still the ordinary one: nothing about this change makes
    // a `review` finding harder to approve.
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled()
  })
})
