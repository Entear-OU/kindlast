'use client'

import { useActionState } from 'react'
import { SendHorizontal } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'

import {
  KINDY_IDLE,
  type KindyAction,
  type KindyState,
  type KindySubject,
} from '@/components/console/kindy-state'
import { MAX_QUESTION_CHARS } from '@/lib/agents/conversation'
import { orgPath } from '@/lib/auth/org'

/**
 * The finding this composer is sitting beside, if it is sitting beside one.
 *
 * The rail is chrome: it is rendered by `[org]/layout.tsx`, which never sees
 * the finding page's `[id]` because a layout does not receive its children's
 * params. The path does, and the path is the one thing that always says what
 * the person is looking at, so it is what the subject comes from (ENT-284).
 *
 * The organisation segment has to match the rail's own, not merely be
 * present. A rail drawn for one organisation must never take a subject from a
 * URL naming another, whatever put that combination on the screen. The action
 * re-resolves the slug against the caller's memberships regardless and would
 * refuse the finding, but a panel that claimed a subject the reader is not
 * looking at would already have misled them by then.
 */
export function subjectFromPath(
  pathname: string | null,
  orgSlug: string,
): string | undefined {
  const segments = (pathname ?? '').split('/').filter(Boolean)
  const [o, slug, section, id, ...rest] = segments
  if (o !== 'o' || slug !== orgSlug) return undefined
  if (section !== 'feed' || !id || rest.length > 0) return undefined
  return id
}

/**
 * The message box on Kindy's card, and the exchange it produces (ENT-270).
 *
 * The first cut was a GET form that navigated to the newest finding's Ask
 * box, and the first person to type "hello" into it was rightly surprised to
 * land on the feed: a message box under a face promises an answer where the
 * message was written. So Kindy answers here now, through the same
 * finding-anchored path, and the subject is a link because an answer nobody
 * can check against its regulation is not one this product gives.
 *
 * # THE SUBJECT IS THE PAGE YOU ARE ON (ENT-284)
 *
 * It used to be whichever finding was newest, chosen by the action, because
 * this form posted the slug and nothing else. Reading the DPO finding and
 * asking about it got you a well-cited answer about the ROPA gap. The box
 * now posts the finding it was rendered beside, and says so above itself
 * before anything is sent, because a wrong subject is only useful to know
 * about before somebody acts on the answer.
 *
 * Away from a finding page it posts nothing and Kindy asks which finding is
 * meant. Both rails carry the same pathname, so the desktop column and the
 * phone's section always agree about the subject.
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
  const subject = subjectFromPath(usePathname(), orgSlug)

  return (
    <div className="mt-auto space-y-3">
      <Exchange
        state={state}
        sending={sending}
        orgSlug={orgSlug}
        submit={submit}
      />

      <div className="rounded-2xl bg-card p-3 shadow-[0_1px_3px_oklch(0_0_0/0.06)]">
        {/* What the next question will be about, before there is an answer
            to act on. The subject is named in the reply too, but a reply is
            read after the answer it sits with, and by then somebody has
            already started believing it. */}
        <p className="px-1 pb-1.5 text-[11px] text-muted-foreground">
          {subject ? (
            <>About the finding on this page.</>
          ) : (
            <>Kindy will ask which finding you mean.</>
          )}
        </p>

        <form action={submit} className="flex items-center gap-2">
          <input type="hidden" name="slug" value={orgSlug} readOnly />
          {/* The subject, and deliberately only its id: the title the panel
              shows is read back from the record by the action, because a
              title posted from here is one anybody can edit. */}
          {subject ? (
            <input type="hidden" name="findingId" value={subject} readOnly />
          ) : null}
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
  submit,
}: {
  state: KindyState
  sending: boolean
  orgSlug: string
  submit: (form: FormData) => void
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
          Kindy is writing. On this deployment&apos;s own model that can take a
          minute or two.
        </p>
      ) : (
        <Reply state={state} orgSlug={orgSlug} submit={submit} />
      )}
    </div>
  )
}

function Reply({
  state,
  orgSlug,
  submit,
}: {
  state: KindyState
  orgSlug: string
  submit: (form: FormData) => void
}) {
  const bubble =
    'mr-6 rounded-2xl rounded-bl-md bg-card px-3 py-2 text-xs leading-relaxed shadow-[0_1px_3px_oklch(0_0_0/0.06)]'

  switch (state.status) {
    case 'idle':
      return null

    case 'answered':
    case 'refused':
      return (
        <div data-testid="kindy-reply" className={bubble}>
          {/* The subject first, above the words it governs. Underneath, it
              is a footnote to an answer already read; above, it is the thing
              that says whether reading on is worth anything (ENT-284). */}
          <Link
            href={orgPath(orgSlug, `/feed/${state.findingId}`)}
            className="mb-1.5 block truncate text-[11px] text-muted-foreground underline underline-offset-2 hover:text-foreground"
          >
            About: {state.findingTitle}
          </Link>
          <p className="text-foreground">
            {state.status === 'answered' ? state.answer : state.reason}
          </p>
          {state.status === 'refused' ? (
            <p className="mt-1 text-[11px] text-muted-foreground">
              That is a guardrail working rather than something broken.
            </p>
          ) : null}
        </div>
      )

    case 'choose':
      return (
        <div data-testid="kindy-reply" className={bubble}>
          {/* Asking, rather than picking the newest and hoping. The question
              rides in the form, so choosing costs one click and never a
              retype. */}
          <p className="text-foreground">
            Which one is that about? I answer about one finding at a time, so
            the regulation behind the answer is checkable.
          </p>
          <form action={submit} className="mt-1.5 space-y-1">
            <input type="hidden" name="slug" value={orgSlug} readOnly />
            <input type="hidden" name="ask" value={state.question} readOnly />
            {state.choices.map((choice: KindySubject) => (
              <button
                key={choice.findingId}
                type="submit"
                name="findingId"
                value={choice.findingId}
                className="block w-full cursor-pointer truncate rounded-lg bg-muted/60 px-2 py-1.5 text-left text-[11px] text-foreground transition-colors hover:bg-muted"
              >
                {choice.findingTitle}
              </button>
            ))}
          </form>
        </div>
      )

    case 'nothing-open':
      return (
        <p data-testid="kindy-reply" className={`${bubble} text-muted-foreground`}>
          Nothing is open to talk about yet. When something lands in Activity,
          ask me about it here.
        </p>
      )

    case 'unavailable':
      return (
        <p data-testid="kindy-reply" className={`${bubble} text-muted-foreground`}>
          This deployment runs no model, so there is nobody behind this box. An
          operator brings one up with the model profile.
        </p>
      )

    case 'error':
      return (
        <p data-testid="kindy-reply" className={`${bubble} text-muted-foreground`}>
          {state.message}
        </p>
      )
  }
}
