import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import FeaturesPage, { metadata } from '@/app/(public)/features/page'

describe('FeaturesPage', () => {
  it('renders a single page-level heading', () => {
    render(<FeaturesPage />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  /**
   * The reason this page was rewritten.
   *
   * It used to advertise a 0-100 compliance score with progress over time, and
   * audit-ready PDF reports. Neither has ever existed: a grep for `pdf` or
   * `compliance_score` across `lib`, `app`, `components` and `supabase` matched
   * the marketing component and nothing else. The repository is public and the
   * rest of the site tells people to go and check it, so a capability list that
   * does not survive a grep is the single most damaging thing that could be on
   * it.
   *
   * This test is a floor, not a style preference. If either capability is ever
   * genuinely built, this assertion should be deleted in the same change that
   * ships it, and not before.
   */
  it('advertises no capability the codebase does not implement', () => {
    const { container } = render(<FeaturesPage />)
    const copy = container.textContent ?? ''

    expect(copy).not.toMatch(/\bPDF\b/i)
    expect(copy).not.toMatch(/compliance score/i)
    expect(copy).not.toMatch(/0 to 100/i)
  })

  it('describes the three registers the product keeps', () => {
    render(<FeaturesPage />)
    expect(screen.getByText(/Records of processing/i)).toBeInTheDocument()
    expect(screen.getByText(/Data subject requests/i)).toBeInTheDocument()
    expect(screen.getByText(/AI systems/i)).toBeInTheDocument()
  })

  it('dissects a real finding rather than illustrating a fake dashboard', () => {
    // The previous version drew a progress-bar panel with four invented
    // percentages and an AI Act tier panel with a "You" badge on Limited Risk.
    // Both were decoration dressed as product data.
    const { container } = render(<FeaturesPage />)
    expect(screen.getByText(/Anatomy of a finding/i)).toBeInTheDocument()
    expect(container.textContent ?? '').not.toMatch(/\b\d{1,3}%/)
  })

  it('names the regulatory corpus it reads from', () => {
    const { container } = render(<FeaturesPage />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/EDPB/)
    expect(copy).toMatch(/enforcement decisions/i)
  })

  it('states the guarantees, including self-hosting under AGPL-3.0', () => {
    const { container } = render(<FeaturesPage />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/AGPL-3\.0/)
    expect(copy).toMatch(/self-host/i)
    expect(copy).toMatch(/audit log/i)
  })

  it('keeps the promise the home page makes when it links here', () => {
    // `CapabilitySummary` on `/` names four areas and offers "See the
    // capabilities in detail". Clicking through used to land on a shorter,
    // different list that mentioned neither ROPA nor DSARs.
    const { container } = render(<FeaturesPage />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/ROPA/)
    expect(copy).toMatch(/DSAR/)
    expect(copy).toMatch(/GDPR/)
    expect(copy).toMatch(/AI Act/)
  })

  it('sends readers on to the pipeline explainer', () => {
    // Assert on the page's OWN call to action, not on any link merely pointing
    // at `/how-it-works`. The footer renders a link literally named "How it
    // works" on every route, so a `/how it works/i` name query matches that
    // instead and would still pass if this page had no onward CTA at all.
    render(<FeaturesPage />)
    const cta = screen.getByRole('link', {
      name: /Follow one finding end to end/i,
    })
    expect(cta).toHaveAttribute('href', '/how-it-works')
  })

  it('distinguishes its own call to action from the footer nav link', () => {
    // Guards the trap above: both links point at the same route, so the only
    // thing separating them is the accessible name. If a future edit reworded
    // the CTA to "How it works", the query above would silently start matching
    // two elements and `getByRole` would throw rather than pass by accident.
    render(<FeaturesPage />)
    const toPipeline = screen
      .getAllByRole('link')
      .filter((el) => el.getAttribute('href') === '/how-it-works')
    expect(toPipeline).toHaveLength(2)
    expect(new Set(toPipeline.map((el) => el.textContent?.trim())).size).toBe(2)
  })

  it('renders the footer', () => {
    render(<FeaturesPage />)
    expect(screen.getByText(/Not legal advice/i)).toBeInTheDocument()
  })

  it('exports route metadata', () => {
    expect(metadata.title).toMatch(/Features/i)
    expect(typeof metadata.description).toBe('string')
  })

  it('carries no waitlist or pricing copy', () => {
    const { container } = render(<FeaturesPage />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/waitlist/i)
    expect(copy).not.toMatch(/\/mo\b/i)
    expect(copy).not.toMatch(/founding-member/i)
    expect(container.innerHTML).not.toMatch(/tally\.so/i)
  })

  it('no longer scopes the audience to SMEs', () => {
    const { container } = render(<FeaturesPage />)
    expect(container.textContent ?? '').not.toMatch(/\bSMEs?\b/)
  })
})
