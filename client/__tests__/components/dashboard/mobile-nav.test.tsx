import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MobileNav } from '@/components/dashboard/mobile-nav'

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  Home: (props: Record<string, unknown>) => <svg data-testid="icon-home" {...props} />,
  Building2: (props: Record<string, unknown>) => <svg data-testid="icon-building" {...props} />,
  List: (props: Record<string, unknown>) => <svg data-testid="icon-list" {...props} />,
  Brain: (props: Record<string, unknown>) => <svg data-testid="icon-brain" {...props} />,
  Download: (props: Record<string, unknown>) => <svg data-testid="icon-download" {...props} />,
  Settings: (props: Record<string, unknown>) => <svg data-testid="icon-settings" {...props} />,
  Menu: (props: Record<string, unknown>) => <svg data-testid="icon-menu" {...props} />,
  XIcon: (props: Record<string, unknown>) => <svg data-testid="icon-x" {...props} />,
  MessageSquare: (props: Record<string, unknown>) => <svg data-testid="icon-message-square" {...props} />,
}))

describe('MobileNav', () => {
  it('renders the hamburger menu button', () => {
    render(<MobileNav />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    expect(menuButton).toBeInTheDocument()
  })

  it('is only visible on mobile (has md:hidden class)', () => {
    render(<MobileNav />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    expect(menuButton.className).toContain('md:hidden')
  })

  it('opens the sheet when hamburger button is clicked', async () => {
    const user = userEvent.setup()
    render(<MobileNav />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    await user.click(menuButton)

    // Sheet should now be open with navigation links
    expect(screen.getByText('Dashboard')).toBeInTheDocument()
    expect(screen.getByText('Compliance Q&A')).toBeInTheDocument()
    expect(screen.getByText('Clients')).toBeInTheDocument()
    expect(screen.getByText('Findings')).toBeInTheDocument()
    expect(screen.getByText('AI Act')).toBeInTheDocument()
    expect(screen.getByText('Export')).toBeInTheDocument()
    expect(screen.getByText('Settings')).toBeInTheDocument()
  })

  it('renders all navigation links with correct hrefs', async () => {
    const user = userEvent.setup()
    render(<MobileNav />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    await user.click(menuButton)

    const dashboardLink = screen.getByText('Dashboard').closest('a')
    expect(dashboardLink).toHaveAttribute('href', '/dashboard')

    const queryLink = screen.getByText('Compliance Q&A').closest('a')
    expect(queryLink).toHaveAttribute('href', '/dashboard/query')

    const clientsLink = screen.getByText('Clients').closest('a')
    expect(clientsLink).toHaveAttribute('href', '/dashboard/clients')

    const findingsLink = screen.getByText('Findings').closest('a')
    expect(findingsLink).toHaveAttribute('href', '/dashboard/findings')

    const aiActLink = screen.getByText('AI Act').closest('a')
    expect(aiActLink).toHaveAttribute('href', '/dashboard/ai-act')

    const exportLink = screen.getByText('Export').closest('a')
    expect(exportLink).toHaveAttribute('href', '/dashboard/export')

    const settingsLink = screen.getByText('Settings').closest('a')
    expect(settingsLink).toHaveAttribute('href', '/dashboard/settings')
  })

  it('shows premium badges on Clients, AI Act and Export', async () => {
    const user = userEvent.setup()
    render(<MobileNav />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    await user.click(menuButton)

    const premiumBadges = screen.getAllByText('Premium')
    expect(premiumBadges).toHaveLength(3)
  })

  it('highlights the active path', async () => {
    const user = userEvent.setup()
    render(<MobileNav activePath="/dashboard/findings" />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    await user.click(menuButton)

    const findingsLink = screen.getByText('Findings').closest('a')
    expect(findingsLink?.className).toContain('active')
  })

  it('displays the Kindlast branding in the sheet header', async () => {
    const user = userEvent.setup()
    render(<MobileNav />)

    const menuButton = screen.getByRole('button', { name: /open menu/i })
    await user.click(menuButton)

    expect(screen.getByText('Kindlast')).toBeInTheDocument()
  })
})
