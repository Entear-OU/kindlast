'use client'

import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
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
  const root = useRef<HTMLDivElement>(null)
  const [handingOff, setHandingOff] = useState<string | null>(null)

  useEffect(() => {
    const element = root.current
    if (!element) return

    const context = gsap.context(() => {
      const media = gsap.matchMedia()

      // Entrance is decorative, so it is the first thing to go under reduced
      // motion: the card is simply already there, at its final position.
      media.add('(prefers-reduced-motion: no-preference)', () => {
        const timeline = gsap.timeline({ defaults: { ease: 'power2.out' } })

        timeline
          .from('[data-seal]', { opacity: 0, scale: 0.92, duration: 0.5 })
          .from('[data-document]', { opacity: 0, y: 8, duration: 0.4 }, '-=0.35')
          // The rule draws rather than fades, which is the one gesture that
          // reads as something being ruled onto a page.
          .from('[data-rule]', { scaleX: 0, transformOrigin: 'left center', duration: 0.45 }, '-=0.2')
          .from('[data-action]', { opacity: 0, y: 6, duration: 0.3, stagger: 0.06 }, '-=0.25')
          .from('[data-assurance]', { opacity: 0, duration: 0.4 }, '-=0.1')
      })

      return () => media.revert()
    }, element)

    return () => context.revert()
  }, [])

  // A full page navigation rather than fetch: the response is a 302 to the
  // authorization server, and the browser has to follow it itself.
  function handOff(href: string, key: string) {
    setHandingOff(key)
    window.location.assign(href)
  }

  return (
    <div ref={root} className="relative w-full max-w-[26rem]">
      {/* Ambient, at the opacity the landing uses: present, not asking to be
          looked at. aria-hidden because it carries no information. */}
      <GuillocheMark
        className="pointer-events-none absolute -top-24 left-1/2 size-[34rem] -translate-x-1/2 opacity-[0.06]"
        durationSeconds={240}
      />

      <div
        data-seal
        aria-hidden="true"
        className="absolute -top-7 -right-7 z-10 grid size-14 place-items-center rounded-full border border-border bg-card shadow-sm"
      >
        <ShieldCheck className="size-6 text-primary" strokeWidth={1.5} />
      </div>

      <div
        data-document
        className="relative rounded-xl border border-border bg-card px-8 py-9 shadow-sm"
      >
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">Sign in</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Continue to your compliance workspace.
        </p>

        <div data-rule className="mt-7 h-px w-full bg-border" />

        <div className="mt-7 space-y-2.5">
          <Button
            data-action
            size="lg"
            className="w-full justify-between"
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
              data-action
              size="lg"
              variant="outline"
              className="w-full"
              disabled={handingOff !== null}
              onClick={() => handOff('/auth/login?idp=google', 'google')}
            >
              {handingOff === 'google' ? <Spinner className="size-4" /> : null}
              Continue with Google
            </Button>
          )}

          <Button
            data-action
            size="lg"
            variant="ghost"
            className="w-full"
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

      <p
        data-assurance
        className="mt-6 text-center font-mono text-[0.6875rem] leading-relaxed tracking-tight text-muted-foreground"
      >
        Kindlast never receives your password.
        <br />
        It is checked by <span className="text-foreground">{issuerHost}</span>.
      </p>
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
