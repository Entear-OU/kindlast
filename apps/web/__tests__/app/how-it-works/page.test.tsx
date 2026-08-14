import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import HowItWorksPage, { metadata } from '@/app/(public)/how-it-works/page'
import { PIPELINE_STAGES } from '@/components/landing/pipeline-stages'

describe('HowItWorksPage', () => {
  it('renders a single page-level heading', () => {
    render(<HowItWorksPage />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('lands the through-line: unprompted, but never unapproved', () => {
    const { container } = render(<HowItWorksPage />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/without being asked/i)
    expect(copy).toMatch(/without approval/i)
  })

  it('names all four agents', () => {
    const { container } = render(<HowItWorksPage />)
    const copy = container.textContent ?? ''
    for (const stage of PIPELINE_STAGES) {
      expect(copy).toContain(stage.agent)
    }
  })

  it('renders the pipeline stages', () => {
    render(<HowItWorksPage />)
    expect(screen.getAllByRole('listitem').length).toBeGreaterThanOrEqual(
      PIPELINE_STAGES.length,
    )
  })

  it('closes with the repository call to action', () => {
    render(<HowItWorksPage />)
    const links = screen.getAllByRole('link', { name: /Read the source/i })
    expect(links.length).toBeGreaterThan(0)
    expect(links[0]).toHaveAttribute(
      'href',
      'https://github.com/Entear-OU/kindlast',
    )
  })

  it('renders the footer', () => {
    render(<HowItWorksPage />)
    expect(screen.getByText(/Not legal advice/i)).toBeInTheDocument()
  })

  it('exports route metadata', () => {
    expect(metadata.title).toMatch(/How it works/i)
    expect(typeof metadata.description).toBe('string')
  })

  it('carries no waitlist or pricing copy', () => {
    const { container } = render(<HowItWorksPage />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/waitlist/i)
    expect(copy).not.toMatch(/\/mo\b/i)
    expect(container.innerHTML).not.toMatch(/tally\.so/i)
  })
})
