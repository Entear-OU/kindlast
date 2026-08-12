import { render } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { TechnicalGrid, DEFAULT_LABELS } from '@/components/landing/technical-grid'

describe('TechnicalGrid', () => {
  it('is decorative and hidden from assistive technology', () => {
    const { container } = render(<TechnicalGrid />)
    const root = container.firstElementChild
    expect(root).toHaveAttribute('aria-hidden', 'true')
    expect(root?.className).toMatch(/pointer-events-none/)
  })

  it('renders the bracketed operating facts, not decorative glyphs', () => {
    // The marginalia has to stay real. If these ever become invented strings
    // the device stops being instrumentation and becomes noise.
    const { container } = render(<TechnicalGrid />)
    const text = container.textContent ?? ''
    expect(text).toContain('[ WATCHER · 06:00 UTC ]')
    expect(text).toContain('[ GDPR ART. 30 ]')
    expect(text).toContain('[ AGPL-3.0 ]')
  })

  it('accepts a caller-supplied label set', () => {
    const { container } = render(
      <TechnicalGrid labels={[{ text: '[ CUSTOM ]', top: '10%', left: '5%' }]} />
    )
    expect(container.textContent).toContain('[ CUSTOM ]')
    expect(container.textContent).not.toContain('[ WATCHER')
  })

  it('draws the rules and the intersection nodes', () => {
    const { container } = render(<TechnicalGrid cell={100} />)
    const html = container.innerHTML
    expect(html).toMatch(/linear-gradient/)
    expect(html).toMatch(/radial-gradient/)
    expect(html).toMatch(/100px 100px/)
  })

  it('inverts for a dark plate', () => {
    // jsdom normalises `rgba()` spacing, so match tolerantly rather than on an
    // exact serialisation we do not control.
    const { container } = render(<TechnicalGrid tone="light" />)
    expect(container.innerHTML).toMatch(/rgba\(\s*255\s*,\s*255\s*,\s*255/)
    expect(container.innerHTML).toMatch(/text-white/)
  })

  it('uses ink rules on the warm ground', () => {
    const { container } = render(<TechnicalGrid tone="dark" />)
    expect(container.innerHTML).toMatch(/rgba\(\s*13\s*,\s*27\s*,\s*42/)
  })

  it('unmounts without throwing', () => {
    const { unmount } = render(<TechnicalGrid />)
    expect(() => unmount()).not.toThrow()
  })

  it('ships a sensible default label set', () => {
    expect(DEFAULT_LABELS.length).toBeGreaterThanOrEqual(4)
    for (const l of DEFAULT_LABELS) {
      expect(l.text.startsWith('[')).toBe(true)
      expect(l.text.endsWith(']')).toBe(true)
    }
  })
})
