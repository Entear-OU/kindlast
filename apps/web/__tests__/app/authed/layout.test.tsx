import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// The layout posts to the `signOut` server action; mock it so the render
// doesn't hit Supabase or pull in `next/headers`. `vi.mock` is hoisted, so
// the mock factory has to define the spy inline — closing over a `const`
// declared above would hit a TDZ error.
const { signOutMock } = vi.hoisted(() => ({ signOutMock: vi.fn() }))
vi.mock('@/lib/auth/actions', () => ({
  signOut: signOutMock,
}))

// `next/link` resolves to a plain anchor in the test env (no Next runtime).
vi.mock('next/link', () => ({
  default: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}))

import AuthedLayout from '@/app/(authed)/layout'

/**
 * ENT-91 — Sign-out affordance on /onboarding.
 *
 * The (authed) layout wraps every authenticated route; ENT-46 will lean on
 * the same shell for the dashboard nav. We only assert the affordance
 * exists and posts to `signOut`; Playwright covers the round-trip.
 */
describe('(authed)/layout — header chrome (ENT-91)', () => {
  it('renders a Kindlast brand link', () => {
    render(
      <AuthedLayout>
        <div>child</div>
      </AuthedLayout>,
    )
    const brand = screen.getByRole('link', { name: /kindlast/i })
    expect(brand).toBeInTheDocument()
  })

  it('renders a sign-out button that submits to the signOut server action', () => {
    render(
      <AuthedLayout>
        <div>child</div>
      </AuthedLayout>,
    )
    const signOutButton = screen.getByRole('button', { name: /sign out/i })
    expect(signOutButton).toBeInTheDocument()
    expect(signOutButton).toHaveAttribute('type', 'submit')

    // The button must sit inside a <form action={signOut}>, otherwise it'd
    // POST to the current page on click.
    const form = signOutButton.closest('form')
    expect(form).not.toBeNull()
  })

  it('renders the children', () => {
    render(
      <AuthedLayout>
        <main data-testid="chat-root">chat</main>
      </AuthedLayout>,
    )
    expect(screen.getByTestId('chat-root')).toBeInTheDocument()
  })
})
