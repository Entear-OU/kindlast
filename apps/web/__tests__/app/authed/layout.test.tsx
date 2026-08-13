import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// The layout posts to /auth/logout through SignOutForm, which mints a CSRF
// token from a cookie. Mocked so the render
// doesn't hit Supabase or pull in `next/headers`. `vi.mock` is hoisted, so
// the mock factory has to define the spy inline — closing over a `const`
// declared above would hit a TDZ error.
// SignOutForm is an async server component: it mints the CSRF token from a
// cookie, which Testing Library cannot render synchronously. Stubbed to the
// shape it produces, so this test stays about the header chrome. The real
// form, including a POST rejected without a token, is covered end to end in
// e2e/auth.spec.ts.
vi.mock('@/components/auth/sign-out-form', () => ({
  SignOutForm: ({ children }: { children: React.ReactNode }) => (
    <form action="/auth/logout" method="post">
      <input type="hidden" name="csrf" value="a-csrf-token" />
      {children}
    </form>
  ),
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
 * exists and posts to /auth/logout; Playwright covers the round-trip.
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

  it('renders a sign-out button that posts to /auth/logout', () => {
    render(
      <AuthedLayout>
        <div>child</div>
      </AuthedLayout>,
    )
    const signOutButton = screen.getByRole('button', { name: /sign out/i })
    expect(signOutButton).toBeInTheDocument()
    expect(signOutButton).toHaveAttribute('type', 'submit')

    // The button must sit inside a POST form to /auth/logout, otherwise it'd
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
