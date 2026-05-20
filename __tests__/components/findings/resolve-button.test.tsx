import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ResolveButton } from '@/components/findings/resolve-button'

describe('ResolveButton', () => {
  it('renders "Mark as Resolved" when not resolved', () => {
    render(<ResolveButton findingId="finding-1" isResolved={false} onToggle={vi.fn()} />)
    expect(screen.getByRole('button', { name: /mark as resolved/i })).toBeInTheDocument()
  })

  it('renders "Mark as Unresolved" when resolved', () => {
    render(<ResolveButton findingId="finding-1" isResolved={true} onToggle={vi.fn()} />)
    expect(screen.getByRole('button', { name: /mark as unresolved/i })).toBeInTheDocument()
  })

  it('calls onToggle with findingId and new resolved state when clicked', async () => {
    const user = userEvent.setup()
    const onToggle = vi.fn()
    render(<ResolveButton findingId="finding-1" isResolved={false} onToggle={onToggle} />)

    await user.click(screen.getByRole('button'))
    expect(onToggle).toHaveBeenCalledWith('finding-1', true)
  })

  it('toggles to unresolved when clicking resolved button', async () => {
    const user = userEvent.setup()
    const onToggle = vi.fn()
    render(<ResolveButton findingId="finding-1" isResolved={true} onToggle={onToggle} />)

    await user.click(screen.getByRole('button'))
    expect(onToggle).toHaveBeenCalledWith('finding-1', false)
  })
})
