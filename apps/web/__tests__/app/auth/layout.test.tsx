import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import AuthLayout from '@/app/(auth)/layout'

describe('AuthLayout', () => {
  it('renders its children', () => {
    render(
      <AuthLayout>
        <p>page body</p>
      </AuthLayout>,
    )
    expect(screen.getByText('page body')).toBeInTheDocument()
  })

  it('keeps the way out on the wordmark', () => {
    render(
      <AuthLayout>
        <p>page body</p>
      </AuthLayout>,
    )
    expect(screen.getByRole('link', { name: 'kindlast' })).toHaveAttribute(
      'href',
      '/',
    )
  })

  it('draws the mark beside the wordmark, and leaves the link one name', () => {
    // The mark is decorative: the wordmark already says what the link is, so
    // an accessible name of "kindlast" is the whole point of hiding it.
    render(
      <AuthLayout>
        <p>page body</p>
      </AuthLayout>,
    )
    const home = screen.getByRole('link', { name: 'kindlast' })
    const mark = home.querySelector('svg')
    expect(mark).not.toBeNull()
    expect(mark).toHaveAttribute('aria-hidden', 'true')
  })
})
