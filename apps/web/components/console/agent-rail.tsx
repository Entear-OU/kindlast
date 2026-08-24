import { Phone, Video, MessageSquare } from 'lucide-react'
import Link from 'next/link'

import { AgentStatusDot } from '@/components/agents/agent-status'
import { AGENTS, STATUS_LABEL } from '@/lib/agents/catalog'
import { orgPath } from '@/lib/auth/org'

/**
 * The three ways of talking to an agent, and which of them exists (ENT-270).
 *
 * Chat does, and it is not a chat window: the Analyst answers about one
 * finding, because a finding names exactly one obligation and offering the run
 * that obligation and nothing else is what lets a citation to anything else be
 * refused. A conversation with no subject would have nothing to check against,
 * so the link goes to the feed, where a subject is.
 *
 * The other two are drawn with the same status vocabulary the agents page uses
 * for the Messenger and the Hands, rather than with a second phrase meaning the
 * same thing. That was already the lesson one level up this file: the rail said
 * one sentence about four agents, and it was wrong the moment one of them
 * moved.
 */
const WAYS_TO_TALK = [
  {
    icon: MessageSquare,
    label: 'Chat',
    status: 'working' as const,
    // What it can be asked, in a person's words. Not "conversational agent":
    // the useful thing to know is that it answers about one finding and not
    // about anything else you might type at it.
    detail: 'Ask why a finding applies to you, on the finding itself.',
  },
  {
    icon: Phone,
    label: 'Call',
    status: 'not-built' as const,
    detail: 'Speaking to an agent would need a voice path that does not exist.',
  },
  {
    icon: Video,
    label: 'Walkthrough',
    status: 'not-built' as const,
    detail: 'Being walked through a record would need one too.',
  },
]

/**
 * The agent rail (ENT-222, made per-agent and addressable by ENT-232).
 *
 * The four agents, in the order work flows through them, drawn as the pipeline
 * they are. Still a miniature of the React Flow canvas the rail grows into:
 * same four nodes, same order, same states, so the canvas is an expansion of
 * something already on screen rather than a new idea.
 *
 * WHY THE STATUS IS THE POINT
 *
 * ENT-161 exists because a dashboard said "Green, you're on track" to a
 * business with three unmet obligations, on a profile the Watcher had never
 * looked at. So the first honest thing this rail can do, long before it can
 * hold a conversation, is answer "is this thing running at all".
 *
 * ENT-222 answered it with one sentence under all four: "Not scheduled yet".
 * That was true when it shipped and stopped being true when ENT-218 landed the
 * Analyst as a real skill on a real harness. A single claim about four
 * different things is wrong as soon as one of them moves, and it moved without
 * anything going red. Each agent now carries its own, from the catalogue, and
 * `__tests__/lib/agents/catalog.test.ts` fails if the console starts describing
 * a skill that does not exist.
 *
 * WHY EACH NAME IS A LINK
 *
 * §26.5 asks for agents addressable by name in the rail. A name that is not a
 * link is a label. Behind each is a page saying when that agent runs and what
 * it is allowed to touch, which are the two questions somebody has when they
 * find out an agent has been reading their compliance record.
 *
 * WHY THE THREE CONTROLS ARE NOT ONE CLAIM ANY MORE
 *
 * Call, chat and video were the direction (ENT-222), none of them existed, and
 * the card said so about all three at once. Chat exists now (ENT-270) and the
 * other two are not close, so a single sentence about the three would be the
 * same failure the per-agent status above was written to fix: a placeholder
 * reading like a feature because it shares a line with one.
 *
 * Each carries its own state, in the same words the agents page uses, and only
 * the one that goes somewhere is a link. See `WAYS_TO_TALK`.
 */

/**
 * Rendered twice, once per layout, because the rail is a column on a wide
 * screen and a section beneath the content on a phone, and those are different
 * places in the DOM rather than the same place styled differently.
 *
 * The `variant` is what keeps that legal: two elements carrying the same `id`
 * is invalid HTML and gives a screen reader two things to land on, so the ids
 * are derived from it. It also gives the phone's tab bar something to link to.
 */
export function AgentRail({
  orgSlug,
  variant = 'desktop',
}: {
  orgSlug: string
  variant?: 'desktop' | 'mobile'
}) {
  const headingId = `agent-rail-heading-${variant}`

  return (
    <aside
      id={variant === 'mobile' ? 'agents' : undefined}
      aria-labelledby={headingId}
      className={
        variant === 'mobile'
          ? 'flex flex-col gap-6 border-t border-border/60 bg-background px-5 py-6'
          : 'flex h-full flex-col gap-6 overflow-y-auto border-l border-border/60 bg-background px-5 py-6'
      }
    >
      <div>
        <h2
          id={headingId}
          className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase"
        >
          Your agents
        </h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Four of them, working in order. Nothing changes without your approval.
        </p>
      </div>

      {/* The pipeline. The connecting line is drawn once behind the list rather
          than per item, so the four read as one flow instead of four cards. */}
      <ol className="relative space-y-5">
        <span
          aria-hidden="true"
          className="absolute top-2 bottom-2 left-[5px] w-px bg-border"
        />
        {AGENTS.map((agent) => (
          <li key={agent.slug} className="relative pl-6">
            <span
              aria-hidden="true"
              className="absolute top-1.5 left-0 size-[11px] rounded-full border-2 border-border bg-background"
            />
            <Link
              href={orgPath(orgSlug, `/agents/${agent.slug}`)}
              className="text-[13px] font-medium text-foreground underline-offset-4 hover:underline"
            >
              {agent.name}
            </Link>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
              {agent.does}
            </p>
            {/* Per agent, because the four are in genuinely different states.
                One shared line here is what went stale last time. */}
            <p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground/80">
              <AgentStatusDot status={agent.status} />
              {STATUS_LABEL[agent.status]}
            </p>
          </li>
        ))}
      </ol>

      {/* The index, so the four are reachable as a set rather than only one at
          a time. Named by the question it answers rather than by the noun:
          "Agents" would repeat the heading above it and tell nobody anything
          they did not already have. */}
      <Link
        href={orgPath(orgSlug, '/agents')}
        className="text-xs text-muted-foreground underline-offset-4 hover:underline"
      >
        What each one is allowed to do
      </Link>

      <div className="mt-auto rounded-lg border border-border/60 bg-muted/40 p-4">
        <p className="text-[13px] font-medium text-foreground">
          Talking to them
        </p>
        <ul className="mt-3 space-y-3">
          {WAYS_TO_TALK.map(({ icon: Icon, label, status, detail }) => (
            <li key={label} className="text-xs">
              <p className="flex items-center gap-1.5 text-foreground">
                <Icon aria-hidden="true" className="size-3.5 shrink-0" />
                {/* A link only for the one that goes somewhere. The other two
                    stay text for the ENT-202 reason: a control that silently
                    does nothing is worse than one visibly absent, and worse
                    here because a person would sit waiting for an answer. */}
                {status === 'working' ? (
                  <Link
                    href={orgPath(orgSlug, '/feed')}
                    className="underline-offset-4 hover:underline"
                  >
                    {label}
                  </Link>
                ) : (
                  label
                )}
              </p>
              <p className="mt-0.5 flex items-center gap-1.5 text-muted-foreground/80">
                <AgentStatusDot status={status} />
                {STATUS_LABEL[status]}
              </p>
              <p className="mt-0.5 leading-relaxed text-muted-foreground">
                {detail}
              </p>
            </li>
          ))}
        </ul>
      </div>
    </aside>
  )
}
