'use client'

import { useState } from 'react'
import { ArrowRight, ShieldCheck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { GuillocheMark } from '@/components/landing/guilloche-mark'

/**
 * The hand-off to the identity provider.
 *
 * There is no password field here and there never will be one. Kindlast has no
 * authentication endpoints at all: the credential is checked by the identity
 * provider, and this application only ever receives a signed assertion about
 * who you are (core-api-surface §1.7). That is the single most reassuring true
 * thing about this screen, so it is the thing the screen says, in the mono
 * register the rest of the product uses for facts rather than for persuasion.
 *
 * The card is treated as a document and the guilloche seal overlaps its top
 * edge, the way a real seal crosses a boundary rather than sitting politely
 * inside one. That motif is the product's own, from the landing hero, rather
 * than an identity invented for this page.
 *
 * # Why the entrance is CSS and not gsap
 *
 * The first version drove it with a gsap timeline of `.from()` tweens, and it
 * shipped the screen blank. `.from()` sets the start state immediately and
 * relies on the tween finishing to reveal anything, so an interrupted timeline
 * leaves the card at opacity 0.073 and the buttons at 0, laid out and
 * invisible, with no error anywhere. React's double-invoked effects in
 * development are enough to cause it.
 *
 * The rule that follows is worth more than the fix: **content must never
 * depend on an animation completing in order to be visible.** These are
 * `animate-in` keyframes from tw-animate-css, the same utilities the dialog
 * and tooltip primitives use. They are pure CSS, they always complete, and
 * with no CSS at all the content is simply there. gsap keeps the rosette,
 * where failure means it stops turning and nothing is lost.
 */
export function SignInCard({
  issuerHost,
  googleEnabled,
  error,
}: {
  /** The host a credential is actually presented to, printed rather than described. */
  issuerHost: string
  googleEnabled: boolean
  error?: string | null
}) {
  const [handingOff, setHandingOff] = useState<string | null>(null)

  // A full page navigation rather than fetch: the response is a 302 to the
  // authorization server, and the browser has to follow it itself.
  function handOff(href: string, key: string) {
    setHandingOff(key)
    window.location.assign(href)
  }

  return (
    <div className="relative w-full max-w-[26rem]">
      {/* Ambient, at the opacity the landing uses: present, not asking to be
          looked at. Decorative, so it carries no accessible name. */}
      <GuillocheMark
        className="pointer-events-none absolute -top-24 left-1/2 size-[34rem] -translate-x-1/2 opacity-[0.06]"
        durationSeconds={240}
      />

      {/* The seal reads as applied to the document rather than printed on it,
          so it carries its own ring and a little more weight than the card's
          hairline. Too light and it is a stray badge floating near a corner. */}
      <div
        data-seal
        aria-hidden="true"
        className="absolute -top-8 -right-8 z-10 grid size-16 origin-center place-items-center rounded-full bg-card shadow-[0_1px_2px_rgba(0,0,0,0.04)] ring-1 ring-border animate-in fade-in-0 zoom-in-95 duration-500 fill-mode-backwards motion-reduce:animate-none"
      >
        <span className="grid size-11 place-items-center rounded-full ring-1 ring-primary/25">
          <ShieldCheck className="size-6 text-primary" strokeWidth={1.75} />
        </span>
      </div>

      <div
        data-document
        className="relative rounded-xl border border-border bg-card px-8 py-9 shadow-sm animate-in fade-in-0 slide-in-from-bottom-2 duration-500 fill-mode-backwards [animation-delay:90ms] motion-reduce:animate-none"
      >
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">Sign in</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Continue to your compliance workspace.
        </p>

        {/* The rule draws rather than fades: the one gesture that reads as
            something being ruled onto a page. */}
        <div
          data-rule
          className="mt-7 h-px w-full origin-left bg-border animate-in fade-in-0 zoom-in-x-50 duration-500 fill-mode-backwards [animation-delay:200ms] motion-reduce:animate-none"
        />

        <div className="mt-7 space-y-2.5">
          <Button
            size="lg"
            className="w-full animate-in fade-in-0 slide-in-from-bottom-1 duration-400 fill-mode-backwards [animation-delay:260ms] motion-reduce:animate-none"
            disabled={handingOff !== null}
            onClick={() => handOff('/auth/login', 'primary')}
          >
            <span>Continue</span>
            {handingOff === 'primary' ? (
              <Spinner className="size-4" />
            ) : (
              <ArrowRight className="size-4" />
            )}
          </Button>

          {googleEnabled && (
            <Button
              size="lg"
              variant="outline"
              className="w-full animate-in fade-in-0 slide-in-from-bottom-1 duration-400 fill-mode-backwards [animation-delay:320ms] motion-reduce:animate-none"
              disabled={handingOff !== null}
              onClick={() => handOff('/auth/login?idp=google', 'google')}
            >
              {handingOff === 'google' ? <Spinner className="size-4" /> : null}
              Continue with Google
            </Button>
          )}

          <Button
            size="lg"
            variant="ghost"
            className="w-full animate-in fade-in-0 duration-400 fill-mode-backwards [animation-delay:380ms] motion-reduce:animate-none"
            disabled={handingOff !== null}
            onClick={() => handOff('/auth/signup', 'signup')}
          >
            Create an account
          </Button>
        </div>

        {error && (
          // Errors state what happened and what to do, in the interface's
          // voice. They do not apologise and they are never vague.
          <p
            role="alert"
            className="mt-6 rounded-lg border border-destructive/30 bg-destructive/5 px-3.5 py-3 text-sm text-foreground"
          >
            {errorMessage(error)}
          </p>
        )}
      </div>

      {/* The signature, so it is allowed to be read. At 11px and muted it was
          a smudge under the card; a footnote rule and a legible size make it
          what someone actually takes away from the screen. */}
      <div
        data-assurance
        className="mt-8 flex flex-col items-center gap-3 animate-in fade-in-0 duration-500 fill-mode-backwards [animation-delay:460ms] motion-reduce:animate-none"
      >
        <span aria-hidden="true" className="h-px w-10 bg-border" />
        <p className="text-center font-mono text-xs leading-relaxed text-foreground/65">
          Kindlast never receives your password.
          <br />
          It is checked by{' '}
          <span className="font-medium text-foreground">{issuerHost}</span>.
        </p>
      </div>
    </div>
  )
}

/**
 * One message per failure the flow can actually produce, each naming the next
 * move. "Something went wrong" tells a person nothing they can act on.
 */
function errorMessage(code: string): string {
  switch (code) {
    case 'state':
      return 'That sign-in link has expired. Start again from this page.'
    case 'denied':
      return 'Sign-in was cancelled. Nothing has changed on your account.'
    case 'exchange':
      return 'The identity provider could not complete sign-in. Try again in a moment.'
    default:
      return 'Sign-in did not complete. Try again from this page.'
  }
}
