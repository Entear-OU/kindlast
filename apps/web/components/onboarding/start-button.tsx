'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { idle, type ActionState } from '@/lib/org/action-state'

/**
 * Opening the interview (ENT-212).
 *
 * A button rather than starting on page load, and the reason is that starting
 * is a write. A GET that opened a session would mean merely looking at this
 * page began an interview, which then shows up as an in-progress session for an
 * organisation whose owner was only having a look.
 */
export function StartButton({
  slug,
  start,
}: {
  slug: string
  start: (
    slug: string,
    previous: ActionState,
    form: FormData,
  ) => Promise<ActionState>
}) {
  const [result, submit, pending] = useActionState(
    async (previous: ActionState, form: FormData) =>
      start(slug, previous, form),
    idle,
  )

  return (
    <form action={submit}>
      <Button type="submit" disabled={pending}>
        {pending ? 'Starting' : 'Start'}
      </Button>
      {result.status === 'error' ? (
        <p className="mt-3 text-sm text-destructive" role="alert">
          {result.message}
        </p>
      ) : null}
    </form>
  )
}
