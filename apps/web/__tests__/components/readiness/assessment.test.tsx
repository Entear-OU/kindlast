import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Assessment } from '@/components/readiness/assessment'
import { OBLIGATIONS, obligationBySlug } from '@/lib/readiness/corpus'
import { SCRIPT } from '@/lib/readiness/script'

/**
 * The readiness assessment, driven end to end (ENT-189).
 *
 * Four properties, and three of them are the ones that would embarrass us:
 *
 *   1. A visitor with no session completes it. There is no account, no token
 *      and no organisation anywhere in the flow, and this proves it rather
 *      than asserting it in a comment.
 *   2. NOTHING IS PERSISTED AND NOTHING IS SENT. Every channel out of the page
 *      is stubbed to throw for the whole run: `fetch`, `XMLHttpRequest`,
 *      `navigator.sendBeacon`, `localStorage`, `sessionStorage`, `document
 *      .cookie` and `history.replaceState`. A test that only asserted "we did
 *      not call fetch" would miss the other six, and the issue's guarantee is
 *      about all of them.
 *   3. The statement of law on the result is the corpus row, character for
 *      character.
 *   4. Nothing the assessment writes for itself asserts law, checked on the
 *      rendered document rather than on the source strings, so a sentence
 *      assembled in JSX is covered too.
 */

/** Every way a page can talk to a server or leave something behind. */
function sealThePage() {
  const escapes: string[] = []
  const trap = (name: string) => () => {
    escapes.push(name)
    throw new Error(`the readiness assessment reached for ${name}`)
  }

  vi.stubGlobal('fetch', trap('fetch'))
  vi.stubGlobal('XMLHttpRequest', function XHRTrap() {
    trap('XMLHttpRequest')()
  })
  vi.stubGlobal('navigator', {
    ...window.navigator,
    sendBeacon: trap('navigator.sendBeacon'),
  })

  const storage = {
    getItem: trap('storage.getItem'),
    setItem: trap('storage.setItem'),
    removeItem: trap('storage.removeItem'),
    clear: trap('storage.clear'),
    key: trap('storage.key'),
    length: 0,
  }
  vi.stubGlobal('localStorage', storage)
  vi.stubGlobal('sessionStorage', storage)

  // `document.cookie` is a property rather than a method, so it needs a
  // descriptor. A setter that throws catches a write; the getter records a
  // read, which is how a page smuggles an identifier back out.
  const cookie = Object.getOwnPropertyDescriptor(Document.prototype, 'cookie')
  Object.defineProperty(document, 'cookie', {
    configurable: true,
    get: () => {
      escapes.push('document.cookie read')
      return ''
    },
    set: trap('document.cookie write'),
  })

  const replaceState = history.replaceState.bind(history)
  history.replaceState = trap('history.replaceState') as never

  return {
    escapes,
    restore() {
      Object.defineProperty(
        document,
        'cookie',
        cookie ?? { configurable: true, value: '' },
      )
      history.replaceState = replaceState
      vi.unstubAllGlobals()
    },
  }
}

/** Answer whatever is on screen until the result appears. */
async function completeTheInterview(user: ReturnType<typeof userEvent.setup>) {
  for (let guard = 0; guard < SCRIPT.length + 4; guard += 1) {
    const continueButton = screen.queryByRole('button', { name: 'Continue' })
    if (continueButton) {
      // A list question. Pick the first real option, which is never a sentinel.
      const options = screen
        .getAllByRole('button', { pressed: false })
        .filter((b) => b.textContent && b.textContent !== 'Continue')
      await user.click(options[0])
      await user.click(screen.getByRole('button', { name: 'Continue' }))
      continue
    }
    const no = screen.queryByRole('button', { name: 'No' })
    if (no) {
      await user.click(no)
      continue
    }
    return
  }
  throw new Error('the interview never finished')
}

describe('the readiness assessment', () => {
  let sealed: ReturnType<typeof sealThePage>

  beforeEach(() => {
    sealed = sealThePage()
  })

  afterEach(() => {
    sealed.restore()
  })

  it('completes with no account, no session and nothing sent anywhere', async () => {
    const user = userEvent.setup()
    render(<Assessment />)

    expect(screen.getByText(SCRIPT[0].prompt)).toBeInTheDocument()
    await completeTheInterview(user)

    expect(
      screen.getByRole('heading', { name: /reached you/i }),
    ).toBeInTheDocument()
    expect(sealed.escapes).toEqual([])
  })

  it('shows the corpus, narrowing, from the first question', async () => {
    const user = userEvent.setup()
    render(<Assessment />)

    const column = screen.getByRole('complementary')
    expect(within(column).getAllByRole('listitem').length).toBe(
      OBLIGATIONS.length,
    )
    // Nothing is set aside before an answer, because nothing has been decided.
    expect(within(column).queryAllByText('Set aside')).toHaveLength(0)

    await completeTheInterview(user)
  })

  it('quotes the corpus verbatim on the result', async () => {
    const user = userEvent.setup()
    render(<Assessment />)
    await completeTheInterview(user)

    // Article 6 binds every controller in the corpus with no narrowing
    // condition, so it reaches a visitor who answered no to everything.
    const article6 = obligationBySlug('gdpr-art-6-lawful-basis')
    expect(article6).toBeDefined()
    expect(screen.getByText(article6!.summary)).toBeInTheDocument()
  })

  it('never says a visitor should have done something', async () => {
    const user = userEvent.setup()
    render(<Assessment />)
    await completeTheInterview(user)

    // The rendered result, not the source strings, so a sentence built in JSX
    // from two safe halves is covered too.
    //
    // Two kinds of node are exempt, and both are exempt STRUCTURALLY rather
    // than by matching their text, because a skip rule that reads the string
    // would also skip a sentence that merely resembles one:
    //
    //   [data-corpus]   the quoted obligation summary, which is the one place
    //                   the law is stated and is not ours to write
    //   [data-citation] a rendered citation, which is a reference rather than
    //                   prose, exactly as `citations.py` treats the field it
    //                   validates separately from the claim
    //
    // Everything else on the page is a sentence somebody here wrote, and none
    // of them may assert law.
    const { assertsLaw } = await import('@/lib/readiness/claims')
    const exempt = '[data-corpus], [data-citation]'

    let checked = 0
    for (const element of document.querySelectorAll('p, li, h2, h3, dt, dd')) {
      if (element.closest(exempt) || element.querySelector(exempt)) continue
      const text = element.textContent?.trim() ?? ''
      if (!text) continue
      checked += 1
      expect(assertsLaw(text), text.slice(0, 160)).toBe(false)
    }

    // Guards the guard. If the exemption selectors ever swallowed the page,
    // every assertion above would vanish and the test would still be green.
    expect(checked).toBeGreaterThan(20)
  })

  it('lets a visitor take back the last answer', async () => {
    const user = userEvent.setup()
    render(<Assessment />)

    // The first question is a list, so answer it and land on the second.
    const options = screen
      .getAllByRole('button', { pressed: false })
      .filter((b) => b.textContent !== 'Continue')
    await user.click(options[0])
    await user.click(screen.getByRole('button', { name: 'Continue' }))
    expect(screen.getByText(SCRIPT[1].prompt)).toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: /change the last answer/i }),
    )
    expect(screen.getByText(SCRIPT[0].prompt)).toBeInTheDocument()
  })

  it('quotes the corpus rather than explaining the law when asked why', async () => {
    const user = userEvent.setup()
    render(<Assessment />)

    // Question two carries a basis, so walk one question forward.
    const options = screen
      .getAllByRole('button', { pressed: false })
      .filter((b) => b.textContent !== 'Continue')
    await user.click(options[0])
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    const why = screen.getByRole('button', { name: /why we ask/i })
    await user.click(why)

    const basis = obligationBySlug(SCRIPT[1].basis!)
    expect(screen.getByText(basis!.summary)).toBeInTheDocument()
    expect(sealed.escapes).toEqual([])
  })
})
