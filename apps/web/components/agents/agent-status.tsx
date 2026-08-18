import type { AgentStatus } from '@/lib/agents/catalog'

/**
 * The dot beside an agent's status (ENT-232).
 *
 * Deliberately not a traffic light, for the reason the posture band gives at
 * length: green means "we looked and it is fine", and none of these three
 * states means that. They mean "this thing exists and runs", "it runs but is
 * not a skill", and "it does not exist". So the palette is neutral and the
 * words carry the meaning.
 *
 * The unbuilt state is a hollow ring rather than a filled dot, because a filled
 * grey dot reads as a state something is in, and "not built" is the absence of
 * one.
 */
const DOT: Record<AgentStatus, string> = {
  working: 'bg-foreground/70',
  'partly-working': 'bg-foreground/30',
  'not-built': 'border border-border bg-transparent',
}

export function AgentStatusDot({ status }: { status: AgentStatus }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block size-[7px] shrink-0 rounded-full ${DOT[status]}`}
    />
  )
}
