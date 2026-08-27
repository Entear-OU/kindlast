'use client'

import { useActionState } from 'react'
import { SendHorizontal } from 'lucide-react'
import Link from 'next/link'

import {
  KINDY_IDLE,
  type KindyAction,
  type KindyState,
} from '@/components/console/kindy-state'
import { MAX_QUESTION_CHARS } from '@/lib/agents/conversation'
import { orgPath } from '@/lib/auth/org'

/**
 * The message box on Kindy's card, and the exchange it produces (ENT-270).
 *
 * The first cut was a GET form that navigated to the newest finding's Ask
 * box, and the first person to type "hello" into it was rightly surprised to
 * land on the feed: a message box under a face promises an answer where the
 * message was written. So Kindy answers here now, through the same
 * finding-anchored path (the action picks the newest open finding and says
 * so in the reply), and the subject is a link because an answer nobody can
 * check against its regulation is not one this product gives.
 *
 * One exchange at a time, kept until the next send. A transcript would
 * promise a memory the backend does not have: every ask is a fresh run.
 */
export function KindyComposer({
  orgSlug,
  action,
  variant,
}: {
  orgSlug: string
  action: KindyAction
  variant: string
}) {
  const [state, submit, sending] = useActionState(action, KINDY_IDLE)

  return (
    <div className="mt-auto space-y-3">
      <Exchange state={state} sending={sending} orgSlug={orgSlug} />

      <div className="rounded-2xl bg-card p-3 shadow-[0_1px_3px_oklch(0_0_0/0.06)]">
        <form action={submit} className="flex items-center gap-2">
          <input type="hidden" name="slug" value={orgSlug} readOnly />
          <label htmlFor={`kindy-ask-${variant}`} className="sr-only">
            Message Kindy
          </label>
          <input
            id={`kindy-ask-${variant}`}
            name="ask"
            required
            maxLength={MAX_QUESTION_CHARS}
            placeholder="Write a message"
            autoComplete="off"
            disabled={sending}
            className="min-w-0 flex-1 bg-transparent px-1 text-xs text-foreground outline-none placeholder:text-muted-foreground/70 disabled:opacity-60"
          />
          <button
            type="submit"
            disabled={sending}
            title="Send to Kindy"
            className="flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-full bg-primary text-primary-foreground transition-opacity hover:opacity-90 disabled:cursor-default disabled:opacity-60"
          >
            <SendHorizontal aria-hidden="true" className="size-3.5" />
            <span className="sr-only">Send to Kindy</span>
          </button>
        </form>
      </div>
    </div>
  )
}

/**
 * The last exchange, as two bubbles: yours, then Kindy's. Every state Kindy
 * can be in produces a sentence to read rather than a spinner that resolves
 * to nothing.
 */
function Exchange({
  state,
  sending,
  orgSlug,
}: {
  state: KindyState
  sending: boolean
  orgSlug: string
}) {
  if (state.status === 'idle' && !sending) return null

  return (
    <div className="space-y-2" aria-live="polite">
      {'question' in state && state.question ? (
        <p className="ml-6 rounded-2xl rounded-br-md bg-primary/10 px-3 py-2 text-xs leading-relaxed text-foreground">
          {state.question}
        </p>
      ) : null}

      {sending ? (
        <p className="mr-6 rounded-2xl rounded-bl-md bg-card px-3 py-2 text-xs leading-relaxed text-muted-foreground shadow-[0_1px_3px_oklch(0_0_0/0.06)]">
          {/* Honest about the wait: on a self-hosted model this is minutes,
              not seconds, and a bare ellipsis reads as broken long before
              the answer lands. */}
          Kindy is writing. On this deployment's own model that can take a
          minute or two.
        </p>
      ) : (
        <Reply state={state} orgSlug={orgSlug} />
      )}
    </div>
  )
}

function Reply({ state, orgSlug }: { state: KindyState; orgSlug: string }) {
  const bubble =
    'mr-6 rounded-2xl rounded-bl-md bg-card px-3 py-2 text-xs leading-relaxed shadow-[0_1px_3px_oklch(0_0_0/0.06)]'

  switch (state.status) {
    case 'idle':
      return null

    case 'answered':
    case 'refused':
      return (
        <div className={bubble}>
          <p className="text-foreground">
            {state.status === 'answered' ? state.answer : state.reason}
          </p>
          {state.status === 'refused' ? (
            <p className="mt-1 text-[11px] text-muted-foreground">
              That is a guardrail working rather than something broken.
            </p>
          ) : null}
          {/* The subject, named and openable: the finding page holds the
              regulation this answer must be checkable against. */}
          <Link
            href={orgPath(orgSlug, `/feed/${state.findingId}`)}
            className="mt-1.5 block truncate text-[11px] text-muted-foreground underline underline-offset-2 hover:text-foreground"
          >
            About: {state.findingTitle}
          </Link>
        </div>
      )

    case 'nothing-open':
      return (
        <p className={`${bubble} text-muted-foreground`}>
          Nothing is open to talk about yet. When something lands in Activity,
          ask me about it here.
        </p>
      )

    case 'unavailable':
      return (
        <p className={`${bubble} text-muted-foreground`}>
          This deployment runs no model, so there is nobody behind this box. An
          operator brings one up with the model profile.
        </p>
      )

    case 'error':
      return (
        <p className={`${bubble} text-muted-foreground`}>{state.message}</p>
      )
  }
}
