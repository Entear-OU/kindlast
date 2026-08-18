import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  CitationLine,
  FindingCard,
  FindingNarrative,
} from '@/components/feed/finding-card'
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

// The same finding after the Analyst got to it (ENT-245 writes these three
// columns and overwrites none of the ones above).
const narrated: Finding = {
  ...finding,
  narrative:
    'You keep customer orders and support tickets, so Article 30 wants a written record of both. Without it a regulator asking what you process has nothing to read.',
  agentRunId: 'run-1',
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

  // ENT-162 and ENT-164 together, and the assertion both issues turn on.
  //
  // The narrative is prose. It goes in the body and the heading stays the short
  // phrase the sweep wrote. Asserting only that the narrative appears somewhere
  // would pass on the exact regression ENT-164 records, so the heading is
  // checked for what it must not contain as well as for what it must.
  it('puts the narrative in the body and never in the heading', () => {
    render(<FindingCard finding={narrated} orgSlug="acme" />)

    const heading = screen.getByRole('heading')
    expect(heading).toHaveTextContent('No record of processing activities')
    expect(heading).not.toHaveTextContent(/Article 30/)

    const narrative = screen.getByTestId('finding-narrative')
    expect(narrative).toHaveTextContent(/Article 30 wants a written record/)
    expect(narrative.tagName).not.toMatch(/^H[1-6]$/)
  })

  // The common case, because narration is a job and most findings have not had
  // one. A card with no narrative renders exactly what it rendered before any
  // of this existed: no empty box, no skeleton, no "narrative pending".
  it('renders a finding with no narrative exactly as it did before', () => {
    render(<FindingCard finding={finding} orgSlug="acme" />)

    expect(screen.queryByTestId('finding-narrative')).not.toBeInTheDocument()
    expect(
      screen.getByText('Create one covering your customer database.'),
    ).toBeInTheDocument()
  })

  // A refusal is a fact about our pipeline rather than about their compliance,
  // so it belongs on the page somebody opened about one finding. A feed where
  // every card reports what we could not do is a feed about us.
  it('keeps a refusal off the card', () => {
    render(
      <FindingCard
        finding={{
          ...finding,
          narrativeRefusal: 'the draft cited GDPR Art. 50',
          agentRunId: 'run-2',
        }}
        orgSlug="acme"
      />,
    )

    expect(screen.queryByText(/Art\. 50/)).not.toBeInTheDocument()
    expect(screen.queryByText(/could not/i)).not.toBeInTheDocument()
  })
})

describe('the narrative on a finding page', () => {
  it('renders the prose and says which agent wrote it', () => {
    render(<FindingNarrative finding={narrated} />)

    expect(
      screen.getByText(/Article 30 wants a written record/),
    ).toBeInTheDocument()
    expect(screen.getByText(/Analyst/)).toBeInTheDocument()
    expect(screen.getByText(/run-1/)).toBeInTheDocument()
  })

  // The refusal surfaces here and only here. "We tried and the model cited an
  // article that does not apply to you" is what somebody deciding whether to
  // trust this product needs to be able to read, and a refusal that leaves no
  // trace is indistinguishable from never having run.
  it('says a run was refused rather than staying silent about it', () => {
    render(
      <FindingNarrative
        finding={{
          ...finding,
          narrativeRefusal: 'the draft cited GDPR Art. 50',
          agentRunId: 'run-2',
        }}
      />,
    )

    expect(screen.getByText(/Art\. 50/)).toBeInTheDocument()
    // And it is explicit that nothing on the page was affected by the attempt,
    // because that is the property that makes a refusal cost nothing.
    expect(screen.getByText(/nothing above/i)).toBeInTheDocument()
  })

  it('renders nothing at all when no run has happened', () => {
    const { container } = render(<FindingNarrative finding={finding} />)
    expect(container).toBeEmptyDOMElement()
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
