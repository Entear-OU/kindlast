import { render } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { HeroLattice } from '@/components/landing/hero-lattice'

/**
 * jsdom has no WebGL context, so every test here runs the exact path a visitor
 * gets on a machine that cannot or will not run WebGL. That is the path that
 * matters most: the hero still has to look finished without it, because the
 * photographic plate underneath is the real content and this is an overlay.
 */
describe('HeroLattice', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders without throwing when WebGL is unavailable', () => {
    expect(() => render(<HeroLattice />)).not.toThrow()
  })

  it('is decorative and hidden from assistive technology', () => {
    const { container } = render(<HeroLattice />)
    const root = container.firstElementChild
    expect(root).toHaveAttribute('aria-hidden', 'true')
  })

  it('stays transparent until the scene is actually running', () => {
    // The canvas fades in only once a frame has been drawn. Without this the
    // reader gets a black rectangle over the plate on a failed init.
    const { container } = render(<HeroLattice />)
    const root = container.firstElementChild as HTMLElement
    expect(root.className).toMatch(/opacity-0/)
  })

  it('unmounts without throwing', () => {
    // Guards the dispose path: a leaked renderer holds a GPU context, and
    // browsers cap how many a page may have.
    const { unmount } = render(<HeroLattice />)
    expect(() => unmount()).not.toThrow()
  })

  it('does not start an animation loop under reduced motion', () => {
    const raf = vi.spyOn(window, 'requestAnimationFrame')
    const original = window.matchMedia
    window.matchMedia = ((q: string) =>
      ({
        matches: q.includes('reduce'),
        media: q,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList) as typeof window.matchMedia

    render(<HeroLattice />)
    expect(raf).not.toHaveBeenCalled()

    window.matchMedia = original
  })
})
