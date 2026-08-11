/**
 * The four agents, described as they are actually implemented.
 *
 * This is the substance behind `/how-it-works`, so it is deliberately a data
 * module rather than JSX buried in a component: the copy is the claim, and it
 * has to be checkable against the code it describes. If an agent's behaviour
 * changes, this file is the thing that has to change with it.
 *
 * The through-line the page has to land is a pair of promises that pull in
 * opposite directions and only make sense together: it runs without being
 * asked, and it never acts without approval.
 */

export interface PipelineFact {
  label: string
  value: string
}

export interface PipelineStage {
  /** Stable id, used for React keys and for the stage anchor. */
  id: string
  /** Two-digit ordinal, rendered as a ghost numeral. */
  index: string
  /** The agent's name in the codebase. */
  agent: string
  /** One-line claim for this stage. */
  headline: string
  /** Body paragraphs, in order. */
  body: string[]
  /** Compact key/value facts, rendered as a definition list. */
  facts: PipelineFact[]
  /** What the tracked signal looks like once this stage has handled it. */
  signal: {
    status: string
    detail: string
  }
}

/**
 * One concrete signal is carried through all four stages so the reader follows
 * a single object rather than four unrelated abstractions. A missing record of
 * processing activity is a good choice: it is a real detector, it maps to a
 * real article (GDPR Art. 30), and the executor genuinely can create the
 * missing entry once a human approves.
 */
export const TRACKED_SIGNAL = {
  dedupKey: 'ropa-gap:marketing-analytics',
  title: 'Marketing analytics has no record of processing.',
} as const

export const PIPELINE_STAGES: PipelineStage[] = [
  {
    id: 'watcher',
    index: '01',
    agent: 'Watcher',
    headline: 'Runs on a schedule, not on a reminder',
    body: [
      'A pg_cron job wakes the watcher once a day. It does not wait to be asked, and it does not ask a model what to look at.',
      'Deterministic detectors sweep three things: regulatory deadlines that are approaching, gaps between your compliance profile and the obligations catalogue, and DSAR response deadlines closing in on a breach.',
      'Each detector emits a finding with a stable dedup_key. Run the sweep again the same day and the key matches, so the open finding is refreshed instead of filed a second time. Nothing accumulates in your inbox just because time passed.',
    ],
    facts: [
      { label: 'Trigger', value: 'pg_cron, daily' },
      { label: 'Logic', value: 'Deterministic detectors, no model' },
      { label: 'Repeats', value: 'Collapsed on dedup_key' },
    ],
    signal: {
      status: 'Detected',
      detail:
        'Found by the daily sweep. Keyed, so tomorrow refreshes it rather than filing another one.',
    },
  },
  {
    id: 'analyst',
    index: '02',
    agent: 'Analyst',
    headline: 'Turns a signal into one specific action',
    body: [
      'A raw signal is not much use on its own. The analyst interprets each one into a structured finding: a plain-language description, the specific GDPR or EU AI Act article it maps to, a severity, an estimated effort, and exactly one proposed action.',
      'An LLM drafts the narrative. A deterministic critic then reads it back and rejects anything vague or non-imperative, and the draft is regenerated until it passes. That is the check that keeps "consider reviewing your processes" off the page.',
    ],
    facts: [
      { label: 'Drafted by', value: 'LLM' },
      { label: 'Checked by', value: 'Deterministic critic' },
      { label: 'Produces', value: 'Article, severity, effort, one action' },
    ],
    signal: {
      status: 'Interpreted',
      detail:
        'GDPR Article 30. High severity, roughly two hours of work, one proposed action.',
    },
  },
  {
    id: 'comms',
    index: '03',
    agent: 'Comms',
    headline: 'Delivered where the decision gets made',
    body: [
      'The finding goes into an outbox and leaves as transactional email. Approve, Reject, and Remind me later are each a single tap, so answering never requires logging in first.',
      'The same outbox carries the weekly Monday briefing and the daily alerts for deadlines that are closing. One channel, three cadences, no dashboard you have to remember to open.',
    ],
    facts: [
      { label: 'Channel', value: 'Transactional email, via an outbox' },
      { label: 'Replies', value: 'Approve, Reject, Remind me later' },
      { label: 'Cadence', value: 'Monday briefing, daily deadline alerts' },
    ],
    signal: {
      status: 'Delivered',
      detail:
        'Sent, with one-tap Approve, Reject, and Remind me later. Now it waits.',
    },
  },
  {
    id: 'executor',
    index: '04',
    agent: 'Executor',
    headline: 'Never acts without approval',
    body: [
      'Until a person approves, nothing happens. The executor has no autonomous path to the database: an explicit human approval is the only thing that starts it, and a finding left unanswered simply stays open.',
      'On approval it performs the action that was proposed and nothing besides, whether that is creating a ROPA entry, logging a DSAR, or registering an AI system. Then it writes an immutable audit log row with a timestamp, so the decision and who made it outlive the person who made it.',
    ],
    facts: [
      { label: 'Trigger', value: 'Explicit human approval' },
      { label: 'Scope', value: 'The approved action, nothing else' },
      { label: 'Record', value: 'Immutable audit log, timestamped' },
    ],
    signal: {
      status: 'Executed',
      detail:
        'ROPA entry created after approval. Audit log row written, timestamped and immutable.',
    },
  },
]
