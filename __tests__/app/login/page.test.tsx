import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

// Mock the auth actions
vi.mock('@/lib/auth/actions', () => ({
  signIn: vi.fn(),
  signUp: vi.fn(),
  signInWithGoogle: vi.fn(),
}))

describe('Login Page', () => {
  it('renders login and sign up tabs', async () => {
    const { default: LoginPage } = await import('@/app/(public)/login/page')
    render(<LoginPage />)

    expect(screen.getByRole('tab', { name: /login/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /sign up/i })).toBeInTheDocument()
  })

  it('shows email and password fields', async () => {
    const { default: LoginPage } = await import('@/app/(public)/login/page')
    render(<LoginPage />)

    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
  })

  it('has Google sign-in button', async () => {
    const { default: LoginPage } = await import('@/app/(public)/login/page')
    render(<LoginPage />)

    expect(screen.getByRole('button', { name: /google/i })).toBeInTheDocument()
  })

  it('has submit button', async () => {
    const { default: LoginPage } = await import('@/app/(public)/login/page')
    render(<LoginPage />)

    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })
})
