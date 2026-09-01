import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// SignOutForm is an async server component that mints a CSRF token from a
// cookie, which Testing Library cannot render synchronously. Stubbed to the
// shape it produces, the way console-shell.test.tsx stubs it.
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

// usePathname is what makes the sidebar and the tab bar client components.
vi.mock('next/navigation', () => ({
  usePathname: () => '/o/acme-ltd/feed/f-1',
}))

import { ConsoleShell } from '@/components/console/shell'
import { KINDY_IDLE, type KindyState } from '@/components/console/kindy-state'

const noopKindy = async (): Promise<KindyState> => KINDY_IDLE

/**
 * The shell must contain what it scrolls (ENT-283).
 *
 * THE BUG. The shell is `h-[100dvh]` and the middle column scrolls inside it,
 * so the document itself should never scroll. On a long finding page it
 * scrolled by 485px, which lifted the whole fixed-height shell off the top of
 * the window and left a white band behind it. Measured at a 1146px viewport on
 * `/o/{slug}/feed/{id}`: `body.scrollHeight` 1146, `documentElement`
 * `.scrollHeight` 1631.
 *
 * The 485px came from a single 1px element. Tailwind's `sr-only` is
 * `position: absolute` with no offsets, so a screen-reader-only label sits at
 * its static position. Nothing in the shell was positioned, so its containing
 * block was the initial containing block (the viewport, in page coordinates)
 * rather than the column it was written inside, and the label for the Ask the
 * Analyst textarea resolved to y=1630.5: outside the 1146px body, and so part
 * of the document's scrollable area. The column it belongs to had scrolled it
 * far down its own content, and with nothing to be contained by it took that
 * offset out to the document.
 *
 * THE PROPERTY. Every absolutely positioned descendant of the shell resolves
 * inside the shell, so none of them can extend the document. In CSS terms: a
 * column that scrolls or clips has to establish a containing block, which is
 * what `relative` does.
 *
 * WHAT THIS TEST CANNOT DO. jsdom has no layout engine and the suite runs with
 * `css: false`, so `scrollHeight` is always 0 and `getComputedStyle` cannot
 * resolve a Tailwind class to `position: relative`. The measurement in the
 * paragraphs above is not reproducible here at all. So these assertions are
 * over class names, one level of indirection away from the property they are
 * about: they hold Tailwind to its own documented meanings (`relative` is
 * `position: relative`, `overflow-y-auto` is a scroll container, `sr-only` is
 * `position: absolute`) and prove the DOM the shell renders satisfies the rule
 * those meanings imply. They would not catch a fix that swapped Tailwind for
 * something else, and they cannot catch the bug returning from a stylesheet.
 * A real measurement belongs in Playwright, where a viewport exists.
 *
 * Both assertions walk the rendered tree rather than naming an element, so a
 * column that is renamed, moved or added is covered without editing this file.
 */

/** Tailwind utilities that make an element a containing block for `absolute`. */
const POSITIONED = /(?:^|\s)(?:relative|absolute|fixed|sticky)(?:\s|$)/

/**
 * Tailwind utilities that scroll or clip. `truncate` and `line-clamp-*` are
 * deliberately not here: they are `overflow: hidden` on a line of text, not a
 * column, and demanding a containing block of every truncated label would be
 * noise.
 */
const CLIPPING =
  /(?:^|\s)overflow(?:-[xy])?-(?:auto|scroll|hidden|clip)(?:\s|$)/

function ancestors(from: Element, upTo: Element): Element[] {
  const chain: Element[] = []
  let node = from.parentElement
  while (node && node !== upTo.parentElement) {
    chain.push(node)
    node = node.parentElement
  }
  return chain
}

function renderShell() {
  const { container } = render(
    <ConsoleShell orgSlug="acme-ltd" kindyAction={noopKindy} orgName="Acme Ltd">
      <main>
        {/* What the finding page renders, in miniature: the label whose
            static position took the document to 1631px. */}
        <label htmlFor="analyst-question" className="sr-only">
          Ask the Analyst about this finding
        </label>
        <textarea id="analyst-question" data-testid="page-content" />
      </main>
    </ConsoleShell>,
  )
  const shell = container.firstElementChild
  if (!(shell instanceof HTMLElement))
    throw new Error('the shell did not render')
  return shell
}

describe('the console shell contains what it scrolls (ENT-283)', () => {
  it('positions the column the page scrolls inside', () => {
    const shell = renderShell()
    const content = shell.querySelector('[data-testid="page-content"]')
    expect(content).not.toBeNull()

    // Found by walking up from the page rather than by class, so this stays
    // about "whatever column the page scrolls in" rather than about one div.
    const scroller = ancestors(content as Element, shell).find((el) =>
      CLIPPING.test(el.className),
    )
    expect(scroller, 'the page is not inside a scrolling column').toBeDefined()
    expect(
      (scroller as Element).className,
      'the scrolling column does not establish a containing block, so an absolutely positioned descendant escapes it and stretches the document',
    ).toMatch(POSITIONED)
  })

  it('leaves no screen-reader-only element resolving against the document', () => {
    const shell = renderShell()
    const srOnly = shell.querySelectorAll('.sr-only')
    // If this ever finds nothing the test has stopped testing anything: the
    // shell's tab bar, the rail's disabled channels and the composer all carry
    // one, and the bug was about exactly these.
    expect(srOnly.length).toBeGreaterThan(0)

    for (const el of srOnly) {
      const contained = ancestors(el, shell).some((ancestor) =>
        POSITIONED.test(ancestor.className),
      )
      expect(
        contained,
        `"${el.textContent}" has no positioned ancestor in the shell, so it resolves against the initial containing block and adds its offset to the document`,
      ).toBe(true)
    }
  })
})
