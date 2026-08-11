import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { OpenSource } from '@/components/landing/open-source'

describe('OpenSource', () => {
  it('renders the section heading', () => {
    render(<OpenSource />)
    // The framing is the North Star: shared compliance infrastructure so
    // European builders ship fast instead of each re-answering the same GDPR
    // questions. Unlike the sections above it the heading is one unbroken
    // string, so it wraps to the measure rather than at a hard-coded <br />.
    expect(
      screen.getByText(/Europe shouldn’t build this twice\./i)
    ).toBeInTheDocument()
  })

  it('names the licence', () => {
    render(<OpenSource />)
    // AGPL-3.0 is the distinguishing fact, not a generic "open source" badge:
    // it is what lets a prospect self-host and what obliges a modified hosted
    // fork to publish its source (ENT-175).
    expect(screen.getAllByText(/AGPL-3\.0/i).length).toBeGreaterThan(0)
  })

  it('renders the repository handle', () => {
    render(<OpenSource />)
    expect(screen.getByText('Entear-OU/kindlast')).toBeInTheDocument()
  })

  it('links to the GitHub repository', () => {
    render(<OpenSource />)
    const link = screen.getByRole('link', { name: /Read the source/i })
    expect(link).toHaveAttribute('href', 'https://github.com/Entear-OU/kindlast')
  })

  it('opens the repository in a new tab safely', () => {
    render(<OpenSource />)
    const link = screen.getByRole('link', { name: /Read the source/i })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('states the three licence guarantees', () => {
    render(<OpenSource />)
    expect(screen.getByText('Auditable')).toBeInTheDocument()
    expect(screen.getByText('Self-hostable')).toBeInTheDocument()
    expect(screen.getByText('Stays open')).toBeInTheDocument()
  })

  it('explains the AGPL section 13 network clause', () => {
    render(<OpenSource />)
    expect(screen.getByText(/section 13/i)).toBeInTheDocument()
  })
})
