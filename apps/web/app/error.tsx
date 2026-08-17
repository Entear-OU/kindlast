'use client'

import { useEffect } from 'react'

import { Button } from '@/components/ui/button'

/**
 * The route error boundary.
 *
 * WHY THE BUTTON IS THE SHARED ONE
 *
 * It was a hand-rolled `<button>` with its own classes, which is how it ended up
 * as the only control in the product with no pointer cursor on hover. Tailwind
 * v4's preflight stopped setting `cursor: pointer` on `button`, which v3 did, so
 * every button now needs it explicitly. `Button` carries it, along with the
 * focus ring, the active state and the disabled handling that the copy here was
 * missing too.
 *
 * That is the general lesson rather than a note about one page: a hand-rolled
 * button does not simply look slightly different, it silently drops the
 * accessibility affordances the shared component exists to guarantee.
 */
export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  // Logged rather than discarded. This boundary is the last place an error is
  // visible, and the digest is the only handle on a server error whose message
  // Next deliberately withholds from the browser. Previously `void _error`,
  // which made a caught error unrecoverable for whoever had to debug it.
  useEffect(() => {
    console.error('route error boundary', error.digest ?? '', error)
  }, [error])

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 px-4 text-center">
      <h1 className="text-2xl font-bold">Something went wrong</h1>
      <p className="text-sm text-muted-foreground">
        An unexpected error occurred. Please try again.
      </p>
      <Button type="button" onClick={reset}>
        Try again
      </Button>
      {error.digest ? (
        // The one thing worth showing: it is what identifies this failure in
        // the server log, and without it a report is "it broke".
        <p className="text-xs text-muted-foreground/70">
          Reference: {error.digest}
        </p>
      ) : null}
    </div>
  )
}
