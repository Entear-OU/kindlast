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
  default: ({
    href,
    children,
    ...rest
  }: {
    href: string
    children: React.ReactNode
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}))

import { ConsoleChrome } from '@/components/console/chrome'

/**
 * The console header (ENT-91, re-homed by ENT-198).
 *
 * It used to be `(authed)/layout.tsx`. That layout became an async server
 * component when organisations moved into the URL, because it has to read a
 * session and resolve a slug before it can render anything, and Testing
 * Library cannot render one synchronously. So the chrome is a plain component
 * taking the organisation as props, and the resolution it sits under is tested
 * as a pure rule in lib/auth/org.test.ts. Two things, each testable, rather
 * than one thing that is neither.
 *
 * The affordances are asserted here; Playwright covers the round trip.
 */
describe('ConsoleChrome (ENT-91, ENT-198)', () => {
  it('points the brand link at the organisation, not at a resolver', () => {
    // The link is "home for this organisation". Sending it to /workspace
    // would make every click on the logo a redirect that has to look up an
    // organisation the page already knows.
    render(
      <ConsoleChrome orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleChrome>,
    )
    const brand = screen.getByRole('link', { name: /kindlast/i })
    expect(brand).toHaveAttribute('href', '/o/acme-ltd')
  })

  it('names the organisation being viewed', () => {
    // With several organisations reachable by URL alone, which one you are
    // looking at has to be visible without reading the address bar.
    render(
      <ConsoleChrome orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleChrome>,
    )
    expect(screen.getByTestId('chrome-org')).toHaveTextContent('Acme Ltd')
  })

  it('renders without an organisation name, for the unavailable state', () => {
    // The layout renders this chrome when core-api cannot be reached, so it
    // has to hold together with nothing but a slug.
    render(
      <ConsoleChrome orgSlug="acme-ltd">
        <div>child</div>
      </ConsoleChrome>,
    )
    expect(screen.getByRole('link', { name: /kindlast/i })).toBeInTheDocument()
    expect(screen.queryByTestId('chrome-org')).toBeNull()
  })

  it('renders a sign-out button that posts to /auth/logout', () => {
    render(
      <ConsoleChrome orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleChrome>,
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
      <ConsoleChrome orgSlug="acme-ltd" orgName="Acme Ltd">
        <main data-testid="chat-root">chat</main>
      </ConsoleChrome>,
    )
    expect(screen.getByTestId('chat-root')).toBeInTheDocument()
  })
})
