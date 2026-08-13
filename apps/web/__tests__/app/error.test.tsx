import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ErrorPage from '@/app/error'

describe('ErrorPage', () => {
  it('renders error message', () => {
    const reset = vi.fn()
    render(<ErrorPage error={new Error('Test error')} reset={reset} />)
    expect(screen.getByText(/something went wrong/i)).toBeInTheDocument()
  })

  it('has a retry button that calls reset', async () => {
    const user = userEvent.setup()
    const reset = vi.fn()
    render(<ErrorPage error={new Error('Test error')} reset={reset} />)
    const button = screen.getByRole('button', { name: /try again/i })
    await user.click(button)
    expect(reset).toHaveBeenCalledOnce()
  })
})
