import { render, screen, within } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { OpenBreakdown } from '@/components/console/open-breakdown'

/**
 * The shape of the queue, which the strip could only ever summarise.
 *
 * The strip says "3, three of them critical or high". That is the count and
 * the urgency, and it cannot say whether the three are one critical and two
 * high or the reverse, which is the difference between a bad week and a very
 * bad one.
 */
describe('OpenBreakdown', () => {
  it('names every severity and its count, including the empty ones', () => {
    // A zero is a fact: "no critical findings" is the sentence a reader most
    // wants, and dropping empty bands would leave them counting absences.
    render(
      <OpenBreakdown counts={{ critical: 2, high: 1, medium: 0, low: 0 }} />,
    )

    for (const [label, count] of [
      ['Critical', '2'],
      ['High', '1'],
      ['Medium', '0'],
      ['Low', '0'],
    ]) {
      // By text and up to its row: `listitem` takes no accessible name from
      // its own content, so querying by role and name finds nothing here.
      const item = screen.getByText(label).closest('li')
      expect(item).not.toBeNull()
      expect(within(item as HTMLElement).getByText(count)).toBeInTheDocument()
    }
  })

  it('sizes each band by its share of the open findings', () => {
    const { container } = render(
      <OpenBreakdown counts={{ critical: 3, high: 1, medium: 0, low: 0 }} />,
    )

    const segments = container.querySelectorAll('[data-severity]')
    const widths = Object.fromEntries(
      [...segments].map((s) => [
        s.getAttribute('data-severity'),
        (s as HTMLElement).style.width,
      ]),
    )
    expect(widths.critical).toBe('75%')
    expect(widths.high).toBe('25%')
    // A band with no findings draws nothing rather than a hairline that reads
    // as one.
    expect(widths.medium).toBeUndefined()
    expect(widths.low).toBeUndefined()
  })

  it('says the queue is empty rather than drawing an empty bar', () => {
    const { container } = render(
      <OpenBreakdown counts={{ critical: 0, high: 0, medium: 0, low: 0 }} />,
    )

    expect(container.querySelectorAll('[data-severity]')).toHaveLength(0)
    expect(screen.getByText(/nothing open/i)).toBeInTheDocument()
  })

  it('carries the number in text, never in colour alone', () => {
    // The same rule the severity badge follows: a reader who cannot separate
    // red from amber still has to be able to read this.
    render(
      <OpenBreakdown counts={{ critical: 2, high: 1, medium: 0, low: 0 }} />,
    )
    const bar = screen.getByTestId('open-breakdown-bar')
    expect(bar).toHaveAttribute('aria-hidden', 'true')
  })

  it('claims no trend, because there is no history to claim one from', () => {
    const { container } = render(
      <OpenBreakdown counts={{ critical: 2, high: 1, medium: 0, low: 0 }} />,
    )
    expect(container.textContent ?? '').not.toMatch(
      /[+-]\d|last (week|month)|since/i,
    )
  })
})
