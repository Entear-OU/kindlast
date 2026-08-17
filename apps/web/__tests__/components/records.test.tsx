import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  CompletenessBadge,
  DueLabel,
  RiskBadge,
  UrgencyBadge,
} from '@/components/records/badges'
import { RegisterNav } from '@/components/records/register-nav'
import {
  AiSystemsTable,
  DsarTable,
  RopaTable,
} from '@/components/records/registers'
import { RegisterUnavailable } from '@/components/records/states'
import type { AiSystem, Dsar, ProcessingActivity } from '@/lib/records/client'

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

describe('the Article 30 register', () => {
  // An Executor stub is mostly empty by design: approve_finding creates the row
  // from a finding that knows the activity exists and not what is in it. The
  // gaps have to read as gaps rather than as a table that failed to render,
  // because Article 30 is a record of fact and its holes are part of what a
  // customer is looking at.
  it('says what is not recorded rather than leaving cells blank', () => {
    const stub: ProcessingActivity = {
      processingActivityId: 'p-1',
      name: 'No record of processing activities exists',
      completeness: 'review_needed',
      sourceFindingId: 'f-1',
    }

    render(<RopaTable items={[stub]} />)

    expect(screen.getAllByText('Not recorded').length).toBeGreaterThan(0)
    expect(screen.getByText('Needs review')).toBeInTheDocument()
  })

  it('renders every Article 30 column it was given', () => {
    const filled: ProcessingActivity = {
      processingActivityId: 'p-2',
      name: 'Payroll',
      purpose: 'Paying staff',
      legalBasis: 'Article 6(1)(b), contract',
      dataCategories: ['name', 'bank details'],
      recipients: ['our accountant'],
      retentionPeriod: '7 years after employment ends',
      completeness: 'complete',
    }

    render(<RopaTable items={[filled]} />)

    expect(screen.getByText('Payroll')).toBeInTheDocument()
    expect(screen.getByText('Article 6(1)(b), contract')).toBeInTheDocument()
    expect(screen.getByText('name, bank details')).toBeInTheDocument()
    expect(screen.getByText('our accountant')).toBeInTheDocument()
    expect(screen.getByText('Complete')).toBeInTheDocument()
    expect(screen.queryByText('Not recorded')).not.toBeInTheDocument()
  })
})

describe('the AI system register', () => {
  const system: AiSystem = {
    aiSystemId: 'a-1',
    name: 'CV ranking model',
    riskClassification: 'unclassified',
    documentationStatus: 'missing',
  }

  // ENT-161's reasoning applied to a record instead of a summary. A register
  // that rendered `unclassified` as anything reassuring would tell a customer
  // they are fine about a system nobody has assessed.
  it('shows an unclassified system as unclassified, not as low risk', () => {
    render(<AiSystemsTable items={[system]} />)

    expect(screen.getByText('Unclassified')).toBeInTheDocument()
    expect(screen.queryByText('Minimal')).not.toBeInTheDocument()
  })

  // Empty vendor is a fact rather than a gap: the AI Act's provider and deployer
  // duties differ, and an organisation that built the system is generally both.
  it('reads an absent supplier as built in house', () => {
    render(<AiSystemsTable items={[system]} />)
    expect(screen.getByText('Built in house')).toBeInTheDocument()
  })

  it('says a system has never been reviewed rather than showing the epoch', () => {
    render(<AiSystemsTable items={[system]} />)

    expect(screen.getByText('Never')).toBeInTheDocument()
    expect(screen.queryByText(/1970/)).not.toBeInTheDocument()
  })
})

describe('the data-subject request log', () => {
  const base: Dsar = {
    dsarId: 'd-1',
    requestType: 'access',
    status: 'open',
    receivedAt: '2026-08-01T12:00:00Z',
    responseDueAt: '2026-08-31T12:00:00Z',
  }

  it('names a request that arrived without a requester', () => {
    render(<DsarTable items={[base]} />)
    expect(screen.getByText('Requester not identified')).toBeInTheDocument()
  })

  it('shows the deadline in the words a handler acts on', () => {
    render(
      <DsarTable items={[{ ...base, urgency: 'due_soon', daysUntilDue: 4 }]} />,
    )
    expect(screen.getByText('Due in 4 days')).toBeInTheDocument()
    expect(screen.getByText('Due soon')).toBeInTheDocument()
  })

  // A response that went out late is still a response. A log that keeps
  // counting is asking somebody to act on something already done.
  it('stops counting once a request has been answered', () => {
    render(
      <DsarTable
        items={[
          {
            ...base,
            status: 'responded',
            respondedAt: '2026-09-10T12:00:00Z',
            urgency: 'answered',
            daysUntilDue: -10,
          },
        ]}
      />,
    )

    expect(screen.getByText('Answered')).toBeInTheDocument()
    expect(screen.queryByText(/overdue/i)).not.toBeInTheDocument()
  })
})

describe('the due label', () => {
  // Reads daysUntilDue, which the server computed by calendar date. These cases
  // pin the wording, including the singulars, because "1 days overdue" is the
  // kind of thing that ships.
  it.each([
    [-11, '11 days overdue'],
    [-1, '1 day overdue'],
    [0, 'Due today'],
    [1, 'Due tomorrow'],
    [4, 'Due in 4 days'],
  ])('renders %d as %s', (days, expected) => {
    render(<DueLabel urgency="on_track" daysUntilDue={days} />)
    expect(screen.getByText(expected)).toBeInTheDocument()
  })
})

describe('badges', () => {
  // An unknown value renders as itself rather than falling back to a default. A
  // register that silently showed a reassuring label for a value it did not
  // recognise would be making a claim nobody made.
  it.each([
    ['completeness', <CompletenessBadge key="c" value="something_new" />],
    ['risk', <RiskBadge key="r" value="something_new" />],
    ['urgency', <UrgencyBadge key="u" value="something_new" />],
  ])('renders an unrecognised %s value as itself', (_name, element) => {
    render(element)
    expect(screen.getByText('something_new')).toBeInTheDocument()
  })

  it('renders nothing at all when the value is absent', () => {
    const { container } = render(<CompletenessBadge />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('the register navigation', () => {
  it('links all three registers and marks the current one', () => {
    render(<RegisterNav slug="acme" active="dsars" />)

    expect(
      screen.getByRole('link', { name: 'Processing activities' }),
    ).toHaveAttribute('href', '/o/acme/records')
    expect(screen.getByRole('link', { name: 'AI systems' })).toHaveAttribute(
      'href',
      '/o/acme/records/ai-systems',
    )

    const current = screen.getByRole('link', { name: 'Data-subject requests' })
    expect(current).toHaveAttribute('href', '/o/acme/records/dsars')
    expect(current).toHaveAttribute('aria-current', 'page')
  })
})

describe('a register that could not be read', () => {
  // ENT-221 landed, so a denial now means a scope an owner can actually grant.
  // The previous copy sent people to a known sign-in gap and told them an owner
  // could not help, which was true then and would be misleading now.
  it('tells a denied reader what would fix it', () => {
    render(
      <RegisterUnavailable
        what="record of processing activities"
        error={{ kind: 'denied', message: 'permission_denied' }}
        testId="records-denied"
      />,
    )

    const message = screen.getByTestId('records-denied')
    expect(message).toHaveTextContent('records:read')
    expect(message).toHaveTextContent(/an owner can grant it/i)
    expect(message).not.toHaveTextContent(/ENT-221/)
  })

  // An empty register is a claim about the customer's compliance record. A
  // failed read must never be rendered as one.
  it('does not read as an empty register', () => {
    render(
      <RegisterUnavailable
        what="AI system register"
        error={{ kind: 'unavailable', message: 'connection refused' }}
        testId="ai-systems-unavailable"
      />,
    )

    const message = screen.getByTestId('ai-systems-unavailable')
    expect(message).toHaveTextContent(/could not be loaded/i)
    expect(message).not.toHaveTextContent(/nothing on file/i)
  })
})
