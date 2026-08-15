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

// usePathname is what makes the sidebar a client component: it marks the
// active surface. Stubbed to the organisation home so the "active" assertions
// have something deterministic to be about.
vi.mock('next/navigation', () => ({
  usePathname: () => '/o/acme-ltd',
}))

import { ConsoleShell } from '@/components/console/shell'

/**
 * The console shell (ENT-91, re-homed by ENT-198, three columns by ENT-222).
 *
 * It used to be `(authed)/layout.tsx`. That layout became an async server
 * component when organisations moved into the URL, because it has to read a
 * session and resolve a slug before it can render anything, and Testing
 * Library cannot render one synchronously. So the shell is a plain component
 * taking the organisation as props, and the resolution it sits under is tested
 * as a pure rule in lib/auth/org.test.ts. Two things, each testable, rather
 * than one thing that is neither.
 *
 * The affordances are asserted here; Playwright covers the round trip.
 *
 * ENT-222 replaced the header with a sidebar, a main column and the agent
 * rail. The assertions that survived the change are the ones that were about
 * behaviour rather than layout, which is the point of having written them that
 * way: the brand still points at the organisation, the organisation is still
 * named, sign-out still posts, and children still render, none of which cared
 * that the chrome moved from the top of the page to the left of it.
 */
describe('ConsoleShell (ENT-91, ENT-198, ENT-222)', () => {
  it('points the brand link at the organisation, not at a resolver', () => {
    // The link is "home for this organisation". Sending it to /workspace
    // would make every click on the logo a redirect that has to look up an
    // organisation the page already knows.
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    const brand = screen.getByRole('link', { name: /kindlast/i })
    expect(brand).toHaveAttribute('href', '/o/acme-ltd')
  })

  it('names the organisation being viewed', () => {
    // With several organisations reachable by URL alone, which one you are
    // looking at has to be visible without reading the address bar.
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.getByTestId('chrome-org')).toHaveTextContent('Acme Ltd')
  })

  it('renders without an organisation name, for the unavailable state', () => {
    // The layout renders this shell when core-api cannot be reached, so it
    // has to hold together with nothing but a slug.
    render(
      <ConsoleShell orgSlug="acme-ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.getByRole('link', { name: /kindlast/i })).toBeInTheDocument()
    expect(screen.queryByTestId('chrome-org')).toBeNull()
  })

  it('renders a sign-out button that posts to /auth/logout', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
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
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <main data-testid="chat-root">chat</main>
      </ConsoleShell>,
    )
    expect(screen.getByTestId('chat-root')).toBeInTheDocument()
  })
})

/**
 * What ENT-222 added, as distinct from what it moved.
 */
describe('the sidebar (ENT-222)', () => {
  it('marks the surface you are on', () => {
    // usePathname is stubbed to the organisation home above.
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.getByRole('link', { name: 'Overview' })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(screen.getByRole('link', { name: 'Settings' })).not.toHaveAttribute(
      'aria-current',
    )
  })

  // The rule from ENT-202, applied to navigation: a control that silently does
  // nothing is worse than one visibly absent, and a nav item leading to a 404
  // is the worst version, because the person concludes the product is broken
  // rather than unfinished.
  it('lists surfaces that do not exist yet without linking them', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.getByText('Feed')).toBeInTheDocument()
    expect(screen.getByText('Records')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Feed' })).toBeNull()
    expect(screen.queryByRole('link', { name: 'Records' })).toBeNull()
  })
})

describe('the agent rail (ENT-222)', () => {
  it('names the four agents as the product names them', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    for (const agent of [
      'The Watcher',
      'The Analyst',
      'The Messenger',
      'The Hands',
    ]) {
      expect(screen.getByText(agent)).toBeInTheDocument()
    }
  })

  // The reason this rail exists before it can hold a conversation. ENT-161
  // happened because a dashboard claimed everything was fine on a profile the
  // Watcher had never looked at. Saying "not scheduled" cannot make that
  // mistake, so it is asserted rather than left to survive a tidy-up.
  it('says plainly that nothing is scheduled, once per agent', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.getAllByText('Not scheduled yet')).toHaveLength(4)
  })

  it('does not render call, chat or video as controls', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    // Announced, not offered. There is no conversational agent behind any of
    // them yet, and a person who pressed one would wait for an answer that is
    // not coming.
    for (const label of ['Chat', 'Call', 'Walkthrough']) {
      expect(screen.getByText(label)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: label })).toBeNull()
      expect(screen.queryByRole('link', { name: label })).toBeNull()
    }
  })
})
