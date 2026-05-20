import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SidebarNav } from '@/components/dashboard/sidebar-nav'

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  Home: (props: Record<string, unknown>) => <svg data-testid="icon-home" {...props} />,
  List: (props: Record<string, unknown>) => <svg data-testid="icon-list" {...props} />,
  Brain: (props: Record<string, unknown>) => <svg data-testid="icon-brain" {...props} />,
  Download: (props: Record<string, unknown>) => <svg data-testid="icon-download" {...props} />,
  Settings: (props: Record<string, unknown>) => <svg data-testid="icon-settings" {...props} />,
}))

describe('SidebarNav', () => {
  it('renders all navigation links', () => {
    render(<SidebarNav />)

    expect(screen.getByText('Dashboard')).toBeInTheDocument()
    expect(screen.getByText('Findings')).toBeInTheDocument()
    expect(screen.getByText('AI Act')).toBeInTheDocument()
    expect(screen.getByText('Export')).toBeInTheDocument()
    expect(screen.getByText('Settings')).toBeInTheDocument()
  })

  it('renders correct links', () => {
    render(<SidebarNav />)

    const dashboardLink = screen.getByText('Dashboard').closest('a')
    expect(dashboardLink).toHaveAttribute('href', '/dashboard')

    const findingsLink = screen.getByText('Findings').closest('a')
    expect(findingsLink).toHaveAttribute('href', '/dashboard/findings')

    const aiActLink = screen.getByText('AI Act').closest('a')
    expect(aiActLink).toHaveAttribute('href', '/dashboard/ai-act')

    const exportLink = screen.getByText('Export').closest('a')
    expect(exportLink).toHaveAttribute('href', '/dashboard/export')

    const settingsLink = screen.getByText('Settings').closest('a')
    expect(settingsLink).toHaveAttribute('href', '/dashboard/settings')
  })

  it('shows premium badges on AI Act and Export', () => {
    render(<SidebarNav />)

    const premiumBadges = screen.getAllByText('Premium')
    expect(premiumBadges).toHaveLength(2)
  })

  it('highlights the active path', () => {
    render(<SidebarNav activePath="/dashboard/findings" />)

    const findingsLink = screen.getByText('Findings').closest('a')
    expect(findingsLink?.className).toContain('active')
  })
})
