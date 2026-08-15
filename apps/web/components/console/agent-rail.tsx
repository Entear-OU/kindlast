import { Phone, Video, MessageSquare } from 'lucide-react'

/**
 * The agent rail (ENT-222).
 *
 * The four agents, in the order work flows through them, drawn as the pipeline
 * they are. This is deliberately a miniature of the React Flow canvas the rail
 * grows into: same four nodes, same order, same states, so the canvas is an
 * expansion of something already on screen rather than a new idea.
 *
 * WHY THE STATUS IS THE POINT
 *
 * The pipeline is invisible today. Signals become findings become records
 * through SQL functions nobody can watch, and the only evidence a human ever
 * gets is a card appearing in a feed. So the first honest thing this rail can
 * do, long before it can hold a conversation, is answer "is this thing running
 * at all".
 *
 * Right now the answer is no, and saying so is the entire value. ENT-161 exists
 * because a dashboard said "Green, you're on track" to a business with three
 * unmet obligations, on a profile the Watcher had never looked at. A rail that
 * says "not scheduled" cannot make that mistake.
 *
 * WHY THE CONTROLS ARE NOT BUTTONS
 *
 * Call, chat and video are the direction (ENT-222) and none of them exist:
 * there is no conversational agent, only scheduled SQL. Rendering them as
 * controls would be the failure ENT-202 named, a control that silently does
 * nothing being worse than one visibly absent, and worse here because a person
 * would sit waiting for an answer that is not coming. They are drawn as what
 * they are: an announcement of what this rail becomes.
 */

/** The agents, named as the product names them publicly. */
const AGENTS = [
  {
    name: 'The Watcher',
    does: 'Reads your profile against the obligations that apply to you.',
  },
  {
    name: 'The Analyst',
    does: 'Turns what it found into a finding that cites the article.',
  },
  {
    name: 'The Messenger',
    does: 'Tells you when something needs a decision.',
  },
  {
    name: 'The Hands',
    does: 'Creates the record, once you have approved it.',
  },
] as const

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
  variant = 'desktop',
}: {
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
          <li key={agent.name} className="relative pl-6">
            <span
              aria-hidden="true"
              className="absolute top-1.5 left-0 size-[11px] rounded-full border-2 border-border bg-background"
            />
            <p className="text-[13px] font-medium text-foreground">
              {agent.name}
            </p>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
              {agent.does}
            </p>
            {/* One shared state for all four, because one thing is true of all
                four: nothing schedules them. When that changes this becomes a
                per-agent last-run time, which is why it is rendered per item
                rather than once at the bottom. */}
            <p className="mt-1 text-xs text-muted-foreground/80">
              Not scheduled yet
            </p>
          </li>
        ))}
      </ol>

      <div className="mt-auto rounded-lg border border-border/60 bg-muted/40 p-4">
        <p className="text-[13px] font-medium text-foreground">
          Talking to them is coming
        </p>
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
          You will be able to ask any of them what they found and why, in
          writing or out loud, and watch them walk you through it.
        </p>
        <ul className="mt-3 flex items-center gap-3 text-muted-foreground/70">
          {[
            { icon: MessageSquare, label: 'Chat' },
            { icon: Phone, label: 'Call' },
            { icon: Video, label: 'Walkthrough' },
          ].map(({ icon: Icon, label }) => (
            <li key={label} className="flex items-center gap-1.5 text-xs">
              <Icon aria-hidden="true" className="size-3.5" />
              {label}
            </li>
          ))}
        </ul>
      </div>
    </aside>
  )
}
