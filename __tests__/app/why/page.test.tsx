import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import WhyPage, { metadata } from '@/app/(public)/why/page'
import { PRINCIPLES } from '@/components/landing/principles'

describe('WhyPage', () => {
  it('renders a single page-level heading stating the thesis', () => {
    render(<WhyPage />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByText(/Move fast\./i)).toBeInTheDocument()
    expect(screen.getByText(/Respect people anyway\./i)).toBeInTheDocument()
  })

  it('names the tension it is refusing', () => {
    // The page exists to reject the trade-off framing, so if this copy ever
    // drifts into generic mission language the page has lost its job.
    render(<WhyPage />)
    const copy = document.body.textContent ?? ''
    expect(copy).toMatch(/pull against each other|trade|framing/i)
    expect(copy).toMatch(/infrastructure/i)
  })

  it('renders every responsible-AI principle', () => {
    render(<WhyPage />)
    for (const p of PRINCIPLES) {
      expect(screen.getByText(p.name)).toBeInTheDocument()
    }
  })

  it('pairs each principle with a mechanism, not just a statement', () => {
    // A principle with nothing enforcing it is a poster. This is the assertion
    // that keeps the section honest.
    render(<WhyPage />)
    for (const p of PRINCIPLES) {
      expect(screen.getByText(p.mechanism)).toBeInTheDocument()
    }
  })

  it('gives every principle an icon', () => {
    const { container } = render(<WhyPage />)
    const icons = container.querySelectorAll('img[src*="/icons/"]')
    expect(icons).toHaveLength(PRINCIPLES.length)
    for (const img of Array.from(icons)) {
      // Decorative: the principle name beside it is the accessible content.
      expect(img).toHaveAttribute('alt', '')
      expect(img).toHaveAttribute('aria-hidden', 'true')
    }
  })

  it('sends readers to the source and to the pipeline', () => {
    render(<WhyPage />)
    expect(
      screen.getByRole('link', { name: /check the mechanisms/i })
    ).toHaveAttribute('href', 'https://github.com/Entear-OU/kindlast')
    expect(
      screen.getByRole('link', { name: /see the pipeline/i })
    ).toHaveAttribute('href', '/how-it-works')
  })

  it('exports route metadata', () => {
    expect(metadata.title).toMatch(/why/i)
    expect(typeof metadata.description).toBe('string')
  })

  it('carries no waitlist or pricing copy', () => {
    const { container } = render(<WhyPage />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/waitlist/i)
    expect(copy).not.toMatch(/\/mo\b/i)
  })
})

describe('PRINCIPLES data', () => {
  it('covers the six core responsible-AI principles', () => {
    expect(PRINCIPLES).toHaveLength(6)
    const names = PRINCIPLES.map((p) => p.name.toLowerCase()).join(' ')
    for (const expected of [
      'fairness',
      'reliability',
      'privacy',
      'transparency',
      'inclusiveness',
      'accountability',
    ]) {
      expect(names).toContain(expected)
    }
  })

  it('gives each principle a distinct icon', () => {
    const icons = PRINCIPLES.map((p) => p.icon)
    expect(new Set(icons).size).toBe(icons.length)
  })
})
