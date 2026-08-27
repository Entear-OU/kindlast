import { render, screen, within } from '@testing-library/react'
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
    // Two brand links now, one per layout: the sidebar's and the phone
    // header's. Both fall back to the wordmark when there is no organisation
    // name to show, which is the state this test is about.
    expect(screen.getAllByRole('link', { name: /kindlast/i })).toHaveLength(2)
    expect(screen.queryByTestId('chrome-org')).toBeNull()
    expect(screen.queryByTestId('mobile-org')).toBeNull()
  })

  it('renders a sign-out button that posts to /auth/logout', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    // One per layout. Both must be real submits inside a POST form, because a
    // sign-out that silently does nothing on a phone is worse than no button:
    // the person believes they have signed out on a shared device.
    const signOutButtons = screen.getAllByRole('button', { name: /sign out/i })
    expect(signOutButtons).toHaveLength(2)

    for (const button of signOutButtons) {
      expect(button).toHaveAttribute('type', 'submit')
      // The button must sit inside a POST form to /auth/logout, otherwise it'd
      // POST to the current page on click.
      expect(button.closest('form')).not.toBeNull()
    }
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
    // Marked in both navigations: the sidebar link and the phone tab, the
    // latter named by aria-label since it renders an icon alone.
    for (const link of screen.getAllByRole('link', { name: 'Overview' })) {
      expect(link).toHaveAttribute('aria-current', 'page')
    }
    for (const link of screen.getAllByRole('link', { name: 'Settings' })) {
      expect(link).not.toHaveAttribute('aria-current')
    }
  })

  // The rule from ENT-202, applied to navigation: a control that silently does
  // nothing is worse than one visibly absent, and a nav item leading to a 404
  // is the worst version, because the person concludes the product is broken
  // rather than unfinished.
  //
  // Nothing is waiting any more. Records was the last entry and left when
  // ENT-200 landed the read surface, so what this now asserts is the other half
  // of the same rule: the heading goes with the list. A "Coming next" heading
  // over nothing reads as a section that failed to load.
  it('shows no coming-next heading when nothing is waiting', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.queryByText('Coming next')).toBeNull()
  })

  // The other half of the same rule, and the one that keeps it honest: a
  // surface leaves the "Coming next" list by becoming a link, not by being
  // quietly deleted from it. Feed made that move in ENT-203.
  it('links a surface once it exists', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    const sidebar = screen.getAllByRole('navigation', { name: 'Console' })[0]
    expect(within(sidebar).getByRole('link', { name: 'Feed' })).toHaveAttribute(
      'href',
      '/o/acme-ltd/feed',
    )
  })
})

describe("the agent rail, now Kindy's panel (ENT-222, ENT-270)", () => {
  // Two instances render, one per layout, and jsdom applies no media queries
  // so both are in the tree. Asserting "two of each" is the honest form: it
  // also fails if a future change drops one layout's rail entirely.
  //
  // This block used to pin the four agents by name and count their statuses.
  // The panel is Kindy now, the agents live behind its "more" button on the
  // agents page, and what the shell has to guarantee moved with the design:
  // Kindy is named, the agents page stays reachable, and the dead channels
  // stay visibly disabled. The finer contract lives with the component, in
  // __tests__/components/agents/agent-rail.test.tsx.
  it('names Kindy and keeps the agents page reachable, in both layouts', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    expect(screen.getAllByText('Kindy')).toHaveLength(2)
    // The kebab is the console's one route to what each agent is allowed to
    // do; losing it orphans that surface (the ENT-245 failure shape).
    for (const link of screen.getAllByRole('link', {
      name: /About Kindy's agents/,
    })) {
      expect(link).toHaveAttribute('href', '/o/acme-ltd/agents')
    }
    expect(
      screen.getAllByRole('link', { name: /About Kindy's agents/ }),
    ).toHaveLength(2)
  })

  it('offers the composer, and keeps call and walkthrough disabled, in both layouts', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    // Chat exists (ENT-270) as the composer, a working form once per layout.
    // Call and walkthrough are visibly disabled rather than live or silent:
    // nothing is behind either, and a person who pressed a live-looking one
    // would wait for an answer that is not coming (ENT-202).
    expect(
      screen.getAllByRole('textbox', { name: /Message Kindy/ }),
    ).toHaveLength(2)

    for (const label of ['Call', 'Walkthrough']) {
      const controls = screen.getAllByRole('button', {
        name: new RegExp(`${label} \\(not built yet\\)`),
      })
      expect(controls).toHaveLength(2)
      for (const control of controls) expect(control).toBeDisabled()
      expect(screen.queryByRole('link', { name: label })).toBeNull()
    }
  })
})

describe('the phone layout (ENT-222)', () => {
  // Icons alone, so the accessible name is the only name there is. A tab bar
  // announcing "link, link, link" is unusable without sight, and the labels
  // cost nothing to carry.
  it('gives every icon-only tab an accessible name', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    const tabBar = screen.getAllByRole('navigation', { name: 'Console' })[1]
    for (const label of ['Overview', 'Settings', 'Your agents']) {
      expect(
        within(tabBar).getByRole('link', { name: label }),
      ).toBeInTheDocument()
    }
  })

  // Same rule as the sidebar, and tighter: a tab bar has no room for a
  // "Coming next" heading, so an unbuilt surface would have to appear as a dead
  // tab, which is exactly the inert control ENT-202 argues against.
  //
  // A surface joins the bar when it becomes real and not before. Feed made that
  // move in ENT-203, Records in ENT-200, and asserting the hrefs rather than
  // mere presence is what stops a tab from graduating to a link that goes
  // nowhere.
  it('gives every built surface a tab that points at it', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    const tabBar = screen.getAllByRole('navigation', { name: 'Console' })[1]

    expect(within(tabBar).getByRole('link', { name: 'Feed' })).toHaveAttribute(
      'href',
      '/o/acme-ltd/feed',
    )
    expect(
      within(tabBar).getByRole('link', { name: 'Records' }),
    ).toHaveAttribute('href', '/o/acme-ltd/records')
  })

  // The rail has no column on a phone, so the tab points at it in the page.
  // Without the anchor the tab is decoration.
  it('points the agents tab at the rail it renders below the content', () => {
    render(
      <ConsoleShell orgSlug="acme-ltd" orgName="Acme Ltd">
        <div>child</div>
      </ConsoleShell>,
    )
    const tabBar = screen.getAllByRole('navigation', { name: 'Console' })[1]
    expect(
      within(tabBar).getByRole('link', { name: 'Your agents' }),
    ).toHaveAttribute('href', '#agents')
  })
})
