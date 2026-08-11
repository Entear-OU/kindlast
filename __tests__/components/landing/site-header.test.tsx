import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { SiteHeader } from '@/components/landing/site-header'

const mockPathname = vi.fn(() => '/')
vi.mock('next/navigation', () => ({
  usePathname: () => mockPathname(),
}))

describe('SiteHeader', () => {
  beforeEach(() => {
    mockPathname.mockReturnValue('/')
  })

  it('renders the primary routes', () => {
    render(<SiteHeader />)
    // Desktop nav plus the mobile panel's copy both render in the DOM; the
    // split is done with CSS, so assert on presence rather than uniqueness.
    const toRoutes = screen
      .getAllByRole('link')
      .map((a) => a.getAttribute('href'))
    expect(toRoutes).toContain('/how-it-works')
    expect(toRoutes).toContain('/features')
  })

  describe('mobile menu', () => {
    it('exposes a labelled toggle that starts closed', () => {
      render(<SiteHeader />)
      const toggle = screen.getByRole('button', { name: /menu/i })
      expect(toggle).toHaveAttribute('aria-expanded', 'false')
      expect(toggle).toHaveAttribute('aria-controls')
    })

    it('does not render the panel until opened', () => {
      render(<SiteHeader />)
      expect(screen.queryByRole('navigation', { name: /mobile/i })).toBeNull()
    })

    it('opens the panel and marks the toggle expanded', async () => {
      const user = userEvent.setup()
      render(<SiteHeader />)
      await user.click(screen.getByRole('button', { name: /menu/i }))

      const toggle = screen.getByRole('button', { name: /menu/i })
      expect(toggle).toHaveAttribute('aria-expanded', 'true')

      const panel = screen.getByRole('navigation', { name: /mobile/i })
      expect(within(panel).getByRole('link', { name: /how it works/i })).toHaveAttribute(
        'href',
        '/how-it-works'
      )
      expect(within(panel).getByRole('link', { name: /features/i })).toHaveAttribute(
        'href',
        '/features'
      )
    })

    it('closes on Escape', async () => {
      const user = userEvent.setup()
      render(<SiteHeader />)
      await user.click(screen.getByRole('button', { name: /menu/i }))
      expect(screen.getByRole('navigation', { name: /mobile/i })).toBeInTheDocument()

      await user.keyboard('{Escape}')
      expect(screen.queryByRole('navigation', { name: /mobile/i })).toBeNull()
    })

    it('closes when a destination is chosen', async () => {
      // Next's client router does not remount the header on navigation, so the
      // panel would otherwise stay open over the page the reader just asked for.
      const user = userEvent.setup()
      render(<SiteHeader />)
      await user.click(screen.getByRole('button', { name: /menu/i }))

      const panel = screen.getByRole('navigation', { name: /mobile/i })
      await user.click(within(panel).getByRole('link', { name: /features/i }))

      expect(screen.queryByRole('navigation', { name: /mobile/i })).toBeNull()
    })

    it('offers the repository call to action inside the panel', () => {
      // The header pill is hidden at these widths, so without this the panel
      // would be the one place a phone reader cannot reach the primary ask.
      render(<SiteHeader />)
      return userEvent
        .setup()
        .click(screen.getByRole('button', { name: /menu/i }))
        .then(() => {
          const panel = screen.getByRole('navigation', { name: /mobile/i })
          expect(
            within(panel).getByRole('link', { name: /read the source/i })
          ).toHaveAttribute('href', 'https://github.com/Entear-OU/kindlast')
        })
    })
  })
})
