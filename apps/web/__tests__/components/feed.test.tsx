import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CitationLine, FindingCard } from '@/components/feed/finding-card'
import { PipelineNote, PostureBand } from '@/components/feed/posture-band'
import type { Finding } from '@/lib/findings/client'

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

const finding: Finding = {
  findingId: 'f-1',
  status: 'pending',
  severity: 'critical',
  detected: 'No record of processing activities',
  proposedAction: 'Create one covering your customer database.',
  citation: {
    label: 'GDPR Art. 30',
    celex: '32016R0679',
    article: 30,
    obligationSlug: 'gdpr-art-30',
  },
}

describe('the posture band', () => {
  // ENT-161, and the reason the band has four states rather than three. The old
  // console counted findings, so an organisation the Watcher had never examined
  // reported "You're on track".
  it('says not assessed rather than green when nothing has run', () => {
    render(
      <PostureBand
        dashboard={{
          posture: 'not_assessed',
          postureHeadline:
            'Not assessed yet. The Watcher has not run for this organisation.',
        }}
      />,
    )

    expect(screen.getByText('Not assessed')).toBeInTheDocument()
    expect(screen.queryByText('On track')).not.toBeInTheDocument()
  })

  // A tally of zero says the same reassuring thing the band is carefully not
  // saying, so it is withheld until something has actually looked.
  it('withholds the counts entirely when nothing has been assessed', () => {
    render(<PostureBand dashboard={{ posture: 'not_assessed' }} />)

    expect(screen.queryByText('Critical')).not.toBeInTheDocument()
    expect(screen.queryByText('High')).not.toBeInTheDocument()
  })

  it('shows the counts once there is an assessment behind them', () => {
    render(
      <PostureBand
        dashboard={{
          posture: 'red',
          postureHeadline: 'Action required.',
          openBySeverity: { critical: 2, high: 1, medium: 0, low: 4 },
        }}
      />,
    )

    expect(screen.getByText('Action required')).toBeInTheDocument()
    expect(screen.getByText('Critical')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('falls back to not assessed for a band it does not recognise', () => {
    // Better a neutral, non-committal state than rendering an unknown value as
    // if it were fine.
    render(<PostureBand dashboard={{ posture: 'chartreuse' }} />)
    expect(screen.getByText('Not assessed')).toBeInTheDocument()
  })
})

describe('the pipeline note', () => {
  // Two different silences with two different fixes: one is waiting for
  // onboarding, the other for a schedule.
  it('distinguishes no profile from no run', () => {
    const { unmount } = render(
      <PipelineNote dashboard={{ posture: 'not_assessed', pipeline: {} }} />,
    )
    expect(screen.getByText(/nothing to check yet/i)).toBeInTheDocument()
    unmount()

    render(
      <PipelineNote
        dashboard={{
          posture: 'not_assessed',
          pipeline: { profileExists: true },
        }}
      />,
    )
    expect(screen.getByText(/has not run yet/i)).toBeInTheDocument()
  })
})

describe('a finding card', () => {
  // ENT-164: the heading is `detected`, what the Watcher observed. The old card
  // put the narrative paragraph here, which made the heading three lines long.
  it('leads with what was detected', () => {
    render(<FindingCard finding={finding} orgSlug="acme" />)

    expect(
      screen.getByText('No record of processing activities'),
    ).toBeInTheDocument()
  })

  it('links to the finding under the organisation slug', () => {
    render(<FindingCard finding={finding} orgSlug="acme" />)

    expect(screen.getByRole('link')).toHaveAttribute('href', '/o/acme/feed/f-1')
  })

  it('names the severity in words, not only in colour', () => {
    render(<FindingCard finding={finding} orgSlug="acme" />)
    expect(screen.getByText('critical')).toBeInTheDocument()
  })
})

describe('a citation', () => {
  // The property the whole product rests on. The label is the one the Analyst
  // stored; nothing here rebuilds it from celex and article.
  it('renders the stored label', () => {
    render(<CitationLine citation={finding.citation} />)
    expect(screen.getByText('GDPR Art. 30')).toBeInTheDocument()
  })

  // The test that matters most. A finding whose label was never stored shows
  // nothing at all, rather than a plausible string assembled from the parts.
  // Silence is recoverable; an invented citation is the one failure this
  // product cannot afford.
  it('renders nothing when no label was stored, even with the parts present', () => {
    const { container } = render(
      <CitationLine citation={{ celex: '32016R0679', article: 30 }} />,
    )

    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText(/Art\. 30/)).not.toBeInTheDocument()
    expect(screen.queryByText(/32016R0679/)).not.toBeInTheDocument()
  })

  it('renders nothing when there is no citation at all', () => {
    const { container } = render(<CitationLine citation={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })
})
