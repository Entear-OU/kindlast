import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

/**
 * `/workspace`, the resolver (ENT-198), and the one thing it used to throw
 * away (ENT-267).
 *
 * The page's ordinary job is to turn "somewhere" into "here": no slug in hand,
 * so resolve the caller's first membership and redirect into it. That is why
 * it was the destination `/invite/{token}` chose when a redemption failed, and
 * why the failure was silent. `?error=invitation` arrived, the page resolved
 * and redirected exactly as it does for anybody else, and the person ended up
 * in their own organisation having been told nothing.
 *
 * So the redirect is now conditional, and both halves are asserted: the
 * ordinary path must still redirect, or every bookmark and every sign-in with
 * no destination lands on a resolver that resolves nothing.
 */

const currentSession = vi.fn()
const loadCurrentUser = vi.fn()
const redirect = vi.fn((to: string) => {
  // Next's redirect throws, and the page's control flow depends on it: without
  // this, a test asserting "redirects" would also render whatever comes after.
  throw new Error(`NEXT_REDIRECT:${to}`)
})

vi.mock('next/navigation', () => ({
  redirect: (to: string) => redirect(to),
}))
vi.mock('@/lib/auth/session', () => ({
  currentSession: () => currentSession(),
}))
vi.mock('@/lib/auth/org', async () => {
  const actual =
    await vi.importActual<typeof import('@/lib/auth/org')>('@/lib/auth/org')
  return {
    ...actual,
    loadCurrentUser: (...a: unknown[]) => loadCurrentUser(...a),
  }
})

const WorkspacePage = (await import('@/app/(authed)/workspace/page')).default

/** The page reads a promise, the way every App Router page now does. */
function query(params: Record<string, string> = {}) {
  return { searchParams: Promise.resolve(params) }
}

const ada = {
  user: { email: 'ada@example.test' },
  memberships: [{ orgId: 'org-1', orgName: 'Acme GmbH', orgSlug: 'acme-gmbh' }],
}

beforeEach(() => {
  vi.clearAllMocks()
  currentSession.mockResolvedValue({ accessToken: 'at-1' })
  loadCurrentUser.mockResolvedValue(ada)
})

describe('/workspace with nothing to report', () => {
  it('resolves the caller into their first organisation', async () => {
    await expect(WorkspacePage(query())).rejects.toThrow(/\/o\/acme-gmbh/)
    expect(redirect).toHaveBeenCalledWith('/o/acme-gmbh')
  })

  // A parameter this page does not know is not a reason to strand somebody on
  // a resolver. Anything but the one value it handles resolves as usual.
  it('ignores an error it does not recognise', async () => {
    await expect(
      WorkspacePage(query({ error: 'something-else' })),
    ).rejects.toThrow(/\/o\/acme-gmbh/)
  })

  it('sends a caller with no session to sign in', async () => {
    currentSession.mockResolvedValue(null)

    await expect(WorkspacePage(query())).rejects.toThrow(/\/sign-in/)
    expect(loadCurrentUser).not.toHaveBeenCalled()
  })
})

describe('/workspace after an invitation could not be used', () => {
  // The bug. This redirected, exactly as above, and said nothing.
  it('stops and explains rather than resolving onward', async () => {
    render(await WorkspacePage(query({ error: 'invitation' })))

    expect(redirect).not.toHaveBeenCalled()
    expect(screen.getByTestId('invitation-failed')).toHaveTextContent(
      /invitation could not be used/i,
    )
  })

  it('names the account it was tried with', async () => {
    render(await WorkspacePage(query({ error: 'invitation' })))

    expect(screen.getByTestId('invitation-failed')).toHaveTextContent(
      'ada@example.test',
    )
  })

  // Explaining must not become stranding. The organisation they do belong to
  // is one click away, which is where the old behaviour dumped them silently.
  it('still offers the organisation they do belong to', async () => {
    render(await WorkspacePage(query({ error: 'invitation' })))

    expect(screen.getByRole('link', { name: /continue/i })).toHaveAttribute(
      'href',
      '/o/acme-gmbh',
    )
  })

  // Reachable, and the worse of the two: somebody whose invitation failed and
  // who holds no membership of their own has nowhere to be redirected to, so
  // the message is the entire page.
  it('explains itself to somebody with no organisation at all', async () => {
    loadCurrentUser.mockResolvedValue({
      user: { email: 'ada@example.test' },
      memberships: [],
    })

    render(await WorkspacePage(query({ error: 'invitation' })))

    expect(redirect).not.toHaveBeenCalled()
    expect(screen.getByTestId('invitation-failed')).toBeInTheDocument()
    expect(screen.queryByRole('link')).toBeNull()
  })

  // core-api being unreachable is not an invitation that worked.
  it('still explains itself when the caller cannot be read', async () => {
    loadCurrentUser.mockResolvedValue(null)

    render(await WorkspacePage(query({ error: 'invitation' })))

    expect(screen.getByTestId('invitation-failed')).toBeInTheDocument()
  })
})
