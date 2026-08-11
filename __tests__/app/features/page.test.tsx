import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import FeaturesPage, { metadata } from '@/app/(public)/features/page'

describe('FeaturesPage', () => {
  it('renders a single page-level heading', () => {
    render(<FeaturesPage />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('renders the capability detail that used to be inline on the home page', () => {
    render(<FeaturesPage />)
    expect(screen.getByText('GDPR Gap Analysis')).toBeInTheDocument()
    expect(screen.getByText('EU AI Act Classification')).toBeInTheDocument()
    expect(screen.getByText('Compliance Score')).toBeInTheDocument()
    expect(screen.getByText('Audit-Ready PDF Reports')).toBeInTheDocument()
    expect(screen.getByText('Actionable Recommendations')).toBeInTheDocument()
    expect(screen.getByText('Privacy-First Architecture')).toBeInTheDocument()
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
})
