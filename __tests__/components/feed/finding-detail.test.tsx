import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { FindingDetailView } from '@/components/feed/finding-detail'
import { severityRationale, type FindingDetail, type SupportingChunk } from '@/lib/feed/finding-detail'

/**
 * ENT-64 — RTL coverage for the finding DETAIL view (presentational only).
 *
 *   * Renders the detected title, proposed action, mapped-obligation link
 *     (href = citation_url), and the severity rationale.
 *   * Pro sees every supporting chunk and no upgrade prompt; Free sees only the
 *     first chunk plus an upgrade prompt naming the locked count.
 *   * Each visible chunk's "View source" link points at its source_url.
 *   * Empty chunks → the "No supporting sources" message.
 */

function finding(over: Partial<FindingDetail> = {}): FindingDetail {
  return {
    id: 'f1',
    detected: 'No DPA on file for Stripe',
    severity: 'high',
    proposed_action: 'Draft a Data Processing Agreement with Stripe.',
    regulatory_obligation: 'GDPR Art. 28',
    citation_url: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_28',
    obligation_slug: 'gdpr-art-28-processor-contracts',
    effort_estimate: 'hours',
    status: 'pending',
    rejection_reason: null,
    snoozed_until: null,
    created_at: '2026-06-01T10:00:00.000Z',
    supporting_context: 'Processors must be bound by a written contract under Art. 28(3).',
    ...over,
  }
}

function chunk(over: Partial<SupportingChunk> = {}): SupportingChunk {
  return {
    ordinal: 1,
    label: 'GDPR Article 28(3)',
    quoted_text: 'Processing by a processor shall be governed by a contract.',
    source_url: 'https://gdpr-info.eu/art-28-gdpr/',
    ...over,
  }
}

describe('FindingDetailView (ENT-64)', () => {
  it('renders the title, description lede, obligation link, and severity rationale', () => {
    render(<FindingDetailView finding={finding()} chunks={[chunk()]} plan="pro" />)

    // ENT-164: the page is titled by the action being proposed. `detected` is
    // prose once the narrative layer has run, so it reads as the lede beneath.
    expect(
      screen.getByRole('heading', {
        level: 1,
        name: 'Draft a Data Processing Agreement with Stripe.',
      }),
    ).toBeInTheDocument()
    expect(screen.getByText('No DPA on file for Stripe')).toBeInTheDocument()

    const link = screen.getByRole('link', { name: 'GDPR Art. 28' })
    expect(link).toHaveAttribute(
      'href',
      'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_28',
    )

    expect(screen.getByText(severityRationale('high'))).toBeInTheDocument()
  })

  it('shows every supporting chunk and no upgrade prompt for Pro', () => {
    const chunks = [
      chunk({ ordinal: 1, label: 'Source one', quoted_text: 'Quote one' }),
      chunk({ ordinal: 2, label: 'Source two', quoted_text: 'Quote two' }),
      chunk({ ordinal: 3, label: 'Source three', quoted_text: 'Quote three' }),
    ]
    render(<FindingDetailView finding={finding()} chunks={chunks} plan="pro" />)

    expect(screen.getByText('Source one')).toBeInTheDocument()
    expect(screen.getByText('Source two')).toBeInTheDocument()
    expect(screen.getByText('Source three')).toBeInTheDocument()
    expect(screen.getAllByText('Quote one').length).toBe(1)
    expect(screen.getAllByRole('blockquote').length).toBe(3)

    expect(screen.queryByText(/Upgrade to Pro/i)).not.toBeInTheDocument()
  })

  it('locks all but the first chunk and shows an upgrade prompt for Free', () => {
    const chunks = [
      chunk({ ordinal: 1, label: 'Source one', quoted_text: 'Quote one' }),
      chunk({ ordinal: 2, label: 'Source two', quoted_text: 'Quote two' }),
      chunk({ ordinal: 3, label: 'Source three', quoted_text: 'Quote three' }),
    ]
    render(<FindingDetailView finding={finding()} chunks={chunks} plan="free" />)

    expect(screen.getByText('Source one')).toBeInTheDocument()
    expect(screen.queryByText('Source two')).not.toBeInTheDocument()
    expect(screen.queryByText('Source three')).not.toBeInTheDocument()

    // Upgrade prompt names the locked count (3 chunks - 1 visible = 2).
    expect(screen.getByText(/2 more/i)).toBeInTheDocument()
    expect(screen.getByText(/Upgrade to Pro/i)).toBeInTheDocument()
  })

  it("points each visible chunk's View source link at its source_url", () => {
    const chunks = [
      chunk({
        ordinal: 1,
        label: 'Source one',
        source_url: 'https://gdpr-info.eu/art-28-gdpr/',
      }),
    ]
    render(<FindingDetailView finding={finding()} chunks={chunks} plan="pro" />)

    const link = screen.getByRole('link', { name: /View source/i })
    expect(link).toHaveAttribute('href', 'https://gdpr-info.eu/art-28-gdpr/')
  })

  it('shows the no-sources message when there are no chunks', () => {
    render(<FindingDetailView finding={finding()} chunks={[]} plan="pro" />)
    expect(screen.getByText(/No supporting sources available/i)).toBeInTheDocument()
  })
})
