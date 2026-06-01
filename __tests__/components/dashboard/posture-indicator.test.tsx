import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PostureIndicator } from '@/components/dashboard/posture-indicator'
import { POSTURE_TOOLTIP } from '@/lib/dashboard/posture'

/**
 * ENT-77 — the Green / Amber / Red indicator. It must be the dominant element,
 * name its band, and explain the score in a tooltip.
 */
describe('PostureIndicator (ENT-77)', () => {
  it('renders the band name as the headline', () => {
    render(<PostureIndicator posture="green" />)
    expect(screen.getByRole('heading', { name: 'Green' })).toBeInTheDocument()
  })

  it('labels the indicator with its band for assistive tech', () => {
    render(<PostureIndicator posture="red" />)
    expect(screen.getByRole('img', { name: /posture: red/i })).toBeInTheDocument()
  })

  it('explains how the score is computed via a tooltip', () => {
    render(<PostureIndicator posture="amber" />)
    expect(screen.getByRole('img', { name: /posture: amber/i })).toHaveAttribute(
      'title',
      POSTURE_TOOLTIP,
    )
    expect(screen.getByText(/how this is scored/i)).toHaveAttribute('title', POSTURE_TOOLTIP)
  })
})
