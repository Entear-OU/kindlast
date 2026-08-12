import { render } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { GuillocheMark } from '@/components/landing/guilloche-mark'

/**
 * The mark is decorative ambience, so the bar is: it renders and stays out of
 * the accessibility tree, and the motion is the first thing dropped when a
 * visitor asks for reduced motion. Tween internals are GSAP's problem, not
 * ours, so nothing here asserts on them.
 */
describe('GuillocheMark', () => {
  const originalMatchMedia = window.matchMedia

  function setReducedMotion(reduce: boolean) {
    window.matchMedia = ((query: string) =>
      ({
        matches: reduce && query.includes('no-preference') ? false : reduce,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList) as typeof window.matchMedia
  }

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    window.matchMedia = originalMatchMedia
  })

  it('renders the rosette as a background image', () => {
    const { container } = render(<GuillocheMark />)
    const el = container.firstElementChild as HTMLElement
    expect(el.getAttribute('style')).toContain('guilloche-rosette.svg')
  })

  it('is hidden from assistive technology', () => {
    const { container } = render(<GuillocheMark />)
    expect(container.firstElementChild).toHaveAttribute('aria-hidden', 'true')
  })

  it('passes through positioning classes', () => {
    const { container } = render(<GuillocheMark className="absolute -top-28" />)
    expect(container.firstElementChild).toHaveClass('absolute', '-top-28')
  })

  it('still renders when the visitor asks for reduced motion', () => {
    // The mark must not disappear; only its rotation is suppressed.
    setReducedMotion(true)
    const { container } = render(<GuillocheMark />)
    const el = container.firstElementChild as HTMLElement
    expect(el).toBeInTheDocument()
    expect(el.getAttribute('style')).toContain('guilloche-rosette.svg')
  })

  it('unmounts without throwing', () => {
    // Guards the gsap.context cleanup path: a leaked repeating tween would
    // keep running against a detached node for the life of the session.
    const { unmount } = render(<GuillocheMark />)
    expect(() => unmount()).not.toThrow()
  })
})
