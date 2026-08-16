'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { idle, type FindingActionState } from '@/lib/findings/action-state'

/**
 * Approve, reject and defer (ENT-203).
 *
 * A client component only because it needs `useActionState` to show the result
 * without a full navigation. The actions themselves are server actions, so no
 * token and no organisation id ever reaches the browser.
 *
 * THREE SEPARATE FORMS, NOT ONE WITH THREE BUTTONS
 *
 * A single form would need the reason field present for every act, and a
 * `reason` posted alongside an approval is a field with nowhere to go. It also
 * keeps each act's pending state its own, so approving does not grey out the
 * defer button.
 */
export function ActControls({
  slug,
  findingId,
  status,
  actions,
}: {
  slug: string
  findingId: string
  status: string
  actions: {
    approve: (
      state: FindingActionState,
      form: FormData,
    ) => Promise<FindingActionState>
    reject: (
      state: FindingActionState,
      form: FormData,
    ) => Promise<FindingActionState>
    snooze: (
      state: FindingActionState,
      form: FormData,
    ) => Promise<FindingActionState>
  }
}) {
  const [approveState, approveAction, approving] = useActionState(
    actions.approve,
    idle,
  )
  const [rejectState, rejectAction, rejecting] = useActionState(
    actions.reject,
    idle,
  )
  const [snoozeState, snoozeAction, snoozing] = useActionState(
    actions.snooze,
    idle,
  )

  // The most recent result wins. Only one act runs at a time in practice, and
  // stacking three messages would leave a stale "Approved." above a fresh
  // error.
  const latest = [snoozeState, rejectState, approveState].find(
    (s) => s.status !== 'idle',
  )

  const settled = status === 'approved' || status === 'rejected'

  return (
    <section
      aria-label="Act on this finding"
      className="rounded-xl border border-border/60 bg-background p-5"
    >
      <h2 className="text-sm font-medium text-foreground">Your decision</h2>

      {settled ? (
        <p className="mt-2 text-sm text-muted-foreground">
          This finding has already been {status}. Acting again changes nothing
          and records nothing.
        </p>
      ) : null}

      <div className="mt-4 space-y-4">
        <form action={approveAction} className="space-y-3">
          <input type="hidden" name="slug" value={slug} />
          <input type="hidden" name="findingId" value={findingId} />

          {/* Recorded on the finding and carried into the audit row. "Approved"
              and "approved having read the regulation" are different claims. */}
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            <input
              type="checkbox"
              name="reviewed"
              className="size-4 cursor-pointer rounded border-border/60"
            />
            I have read the regulation this cites
          </label>

          <Button type="submit" disabled={approving || settled}>
            {approving ? 'Approving…' : 'Approve'}
          </Button>
        </form>

        <form action={rejectAction} className="space-y-3">
          <input type="hidden" name="slug" value={slug} />
          <input type="hidden" name="findingId" value={findingId} />

          <label className="block text-sm text-muted-foreground">
            Why this does not apply
            <input
              type="text"
              name="reason"
              placeholder="Optional, and worth saying"
              className="mt-1 w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/60"
            />
          </label>
          {/* Not decoration: three rejections of the same obligation raise a
              product review flag carrying these reasons, which is how a bad
              rule gets noticed rather than quietly dismissed by everyone. */}

          <Button
            type="submit"
            variant="outline"
            disabled={rejecting || settled}
          >
            {rejecting ? 'Rejecting…' : 'Reject'}
          </Button>
        </form>

        <form action={snoozeAction} className="flex items-end gap-2">
          <input type="hidden" name="slug" value={slug} />
          <input type="hidden" name="findingId" value={findingId} />

          <label className="text-sm text-muted-foreground">
            Defer for
            <input
              type="number"
              name="days"
              defaultValue={7}
              min={1}
              max={365}
              className="ml-2 w-20 rounded-lg border border-border/60 bg-background px-2 py-1.5 text-sm text-foreground"
            />
            <span className="ml-2">days</span>
          </label>

          <Button type="submit" variant="ghost" disabled={snoozing}>
            {snoozing ? 'Deferring…' : 'Defer'}
          </Button>
        </form>
      </div>

      {latest ? (
        <p
          role="status"
          data-testid="act-result"
          className={`mt-4 text-sm ${
            latest.status === 'error' ? 'text-red-300' : 'text-muted-foreground'
          }`}
        >
          {latest.message}
        </p>
      ) : null}
    </section>
  )
}
