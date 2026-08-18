import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'

import ApproveFromEmailPage from '@/app/approve/[findingId]/[token]/page'

/**
 * The interstitial a one-tap approve link lands on (§8, ENT-249).
 *
 * THE ONE PROPERTY THIS FILE EXISTS FOR: rendering this page changes nothing.
 * A mail scanner fetching the URL gets a page and nothing else, so the act of
 * delivering a finding notification cannot approve the finding. The assertion
 * is not about markup, it is that nothing behind the page was called: no
 * fetch, so no redemption, so no approval, so no audit row.
 *
 * The second property is that it says what it is doing. Somebody approving
 * from a mailbox has not read the finding and `approval_reviewed` will record
 * exactly that, so the page has to say so before the click rather than the
 * trail saying it afterwards.
 *
 * PROVEN ABLE TO FAIL. Making the page validate the token on the way in, which
 * is the obvious "helpful" change somebody will propose, turns the first test
 * red. It would also turn this page into an oracle for which credentials are
 * live, to a caller who has proved nothing.
 */

const params = Promise.resolve({ findingId: 'f-123', token: 'deleg-123' })

afterEach(() => {
  vi.restoreAllMocks()
})

describe('the approve-from-email interstitial', () => {
  it('renders without calling anything at all', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')

    render(await ApproveFromEmailPage({ params }))

    expect(fetchSpy).not.toHaveBeenCalled()
    expect(
      screen.getByRole('heading', { name: /approve this finding\?/i }),
    ).toBeInTheDocument()
  })

  it('says that approving from here is approving without reading', async () => {
    render(await ApproveFromEmailPage({ params }))

    expect(screen.getByText(/have not read the finding here/i)).toBeVisible()
    expect(screen.getByText(/audit trail will say so/i)).toBeVisible()
  })

  it('tells somebody with a stale link where to go instead', async () => {
    // The cost of the one hour ceiling, paid here. A delegation may not be
    // long-lived, so an approve link read the next morning will not work, and
    // the honest answer is one more click rather than a dead end.
    render(await ApproveFromEmailPage({ params }))

    expect(
      screen.getByText(/works once and expires within the hour/i),
    ).toBeVisible()
  })
})
