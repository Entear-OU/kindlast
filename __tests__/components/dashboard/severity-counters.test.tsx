import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { SeverityCounters } from '@/components/dashboard/severity-counters'
import { countOpenBySeverity } from '@/lib/dashboard/severity'

/**
 * ENT-78 — the four severity counters and their pre-filtered feed links.
 */
describe('SeverityCounters (ENT-78)', () => {
  const counts = countOpenBySeverity(['critical', 'high', 'high'])

  it('renders all four counters', () => {
    render(<SeverityCounters counts={counts} />)
    for (const label of ['Critical', 'High', 'Medium', 'Low']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
  })

  it('links each counter to the feed pre-filtered by its severity', () => {
    render(<SeverityCounters counts={counts} />)
    expect(screen.getByRole('link', { name: /critical: 1 open/i })).toHaveAttribute(
      'href',
      '/feed?severity=critical',
    )
    expect(screen.getByRole('link', { name: /high: 2 open/i })).toHaveAttribute(
      'href',
      '/feed?severity=high',
    )
    expect(screen.getByRole('link', { name: /medium: 0 open/i })).toHaveAttribute(
      'href',
      '/feed?severity=medium',
    )
  })
})
