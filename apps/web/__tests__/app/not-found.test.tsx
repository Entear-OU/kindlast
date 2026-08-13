import type { ComponentProps } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import NotFound from '@/app/not-found'

vi.mock('next/link', () => ({
  default: ({ children, href, ...props }: ComponentProps<'a'>) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

describe('NotFound', () => {
  it('renders 404 message', () => {
    render(<NotFound />)
    expect(screen.getByText(/not found/i)).toBeInTheDocument()
  })

  it('has a link back to home', () => {
    render(<NotFound />)
    const link = screen.getByRole('link', { name: /home/i })
    expect(link).toHaveAttribute('href', '/')
  })
})
