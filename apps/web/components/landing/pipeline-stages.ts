/**
 * The four agents, in the order a problem passes through them.
 *
 * This is the substance behind `/how-it-works`, so it is a data module rather
 * than JSX buried in a component: the copy is the claim, and it has to be
 * checkable against the code it describes. If an agent's behaviour changes,
 * this file changes with it.
 *
 * The copy leads in plain language and keeps the engineering underneath.
 * An earlier version led with `pg_cron`, `dedup_key` and "deterministic
 * critic", which is precise but reads as release notes to the founder actually
 * deciding whether to trust this. The technical claim still matters, because
 * the repository is public and anyone can check it, so it stays as a demoted
 * `technical` line rather than being cut.
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
  /** The single sentence a reader should take away. Leads the stage. */
  plain: string
  /** Body paragraphs, in order. Plain language, no jargon. */
  body: string[]
  /** How it is actually built. Demoted, for readers who want to verify. */
  technical: string
  /** Compact key/value facts, rendered as a definition list. */
  facts: PipelineFact[]
  /** What the tracked signal looks like once this stage has handled it. */
  signal: {
    status: string
    detail: string
  }
}

/**
 * One concrete problem is carried through all four stages so the reader follows
 * a single thing rather than four unrelated abstractions. A missing record of
 * processing is a good choice: it is a real detector, it maps to a real article
 * (GDPR Art. 30), and the executor genuinely can create the missing entry once
 * a human approves.
 */
export const TRACKED_SIGNAL = {
  dedupKey: 'ropa-gap:marketing-analytics',
  title: 'Marketing analytics has no record of processing.',
} as const

export const PIPELINE_STAGES: PipelineStage[] = [
  {
    id: 'watcher',
    index: '01',
    agent: 'The Watcher',
    headline: 'It checks every day, so nobody has to remember to',
    plain:
      'Every day it looks through your compliance record for problems, without being asked.',
    body: [
      'Once a day it goes looking for three kinds of trouble: deadlines coming up, things the law expects you to have that you do not, and customer data requests that are running out of time to answer.',
      'Nothing triggers it. You do not open an app or start a scan. This is the difference between compliance you remember to do and compliance that happens.',
      'If it finds the same problem again tomorrow, it updates the one you already have rather than sending you a second copy. Your inbox does not fill up just because time passed.',
    ],
    technical:
      'Called the Watcher in the repository. A pg_cron job runs the sweep daily; the detectors are deterministic SQL with no model in the loop, and each finding carries a stable dedup key so repeat runs collapse onto the open finding rather than duplicating it.',
    facts: [
      { label: 'How often', value: 'Every day' },
      { label: 'What decides', value: 'Fixed rules, not AI' },
      { label: 'Repeat problems', value: 'Updated, never duplicated' },
    ],
    signal: {
      status: 'Found',
      detail:
        'Picked up by the daily check. If it is still there tomorrow, this same item updates.',
    },
  },
  {
    id: 'analyst',
    index: '02',
    agent: 'The Analyst',
    headline: 'It works out what the problem means and what to do',
    plain: 'It turns a raw signal into one specific thing you can actually do.',
    body: [
      'Knowing something is wrong is not much use on its own. This step works out what it means: what the problem is in plain English, which specific law it relates to, how serious it is, roughly how long it will take to fix, and exactly one thing to do about it.',
      'Vague advice never reaches you. If the draft says something like "consider reviewing your processes", it is rejected and rewritten until it says something you could actually sit down and do.',
    ],
    technical:
      'Called the Analyst in the repository. A language model drafts the explanation, then a deterministic critic reads it back and rejects anything vague or non-imperative, regenerating until it passes. The critic is ordinary code, not another model.',
    facts: [
      { label: 'Written by', value: 'AI' },
      { label: 'Checked by', value: 'Fixed rules' },
      { label: 'You get', value: 'The law, the severity, one action' },
    ],
    signal: {
      status: 'Explained',
      detail:
        'Tied to GDPR Article 30. Serious, roughly two hours of work, with one thing to do.',
    },
  },
  {
    id: 'comms',
    index: '03',
    agent: 'The Messenger',
    headline: 'It comes to you, rather than waiting to be found',
    plain: 'The answer arrives in your inbox, and replying takes one tap.',
    body: [
      'You get an email. Approve, Reject, and Remind me later are one tap each, so answering never means logging in first or finding the right page.',
      'There is no dashboard you have to remember to check. The same channel sends a short summary on Monday and a warning when a deadline is getting close, so the only thing you have to keep up with is your email.',
    ],
    technical:
      'Called Comms in the repository. Delivery goes through an outbox as transactional email, with signed one-tap action links so a reply is authenticated without a session.',
    facts: [
      { label: 'Where it lands', value: 'Your inbox' },
      { label: 'To answer', value: 'One tap' },
      { label: 'Also sends', value: 'Monday summary, deadline warnings' },
    ],
    signal: {
      status: 'Sent to you',
      detail:
        'In your inbox, with Approve, Reject, and Remind me later. Now it waits for you.',
    },
  },
  {
    id: 'executor',
    index: '04',
    agent: 'The Hands',
    headline: 'It does nothing until you say yes',
    plain:
      'Nothing changes unless you approve it, and everything that happens is written down.',
    body: [
      'Until you approve, nothing happens. There is no route for it to act on its own, and a finding you ignore simply stays open. This is deliberate: an agent that can change your records unsupervised is a liability, not a help.',
      'When you do approve, it does the one thing that was proposed and nothing else, then records what happened, when, and that you approved it. That record cannot be edited afterwards, which is what makes it worth anything to an auditor.',
    ],
    technical:
      'Called the Executor in the repository. There is no autonomous write path: an explicit approval is the only trigger, and it writes an immutable audit log row carrying the actor and a timestamp.',
    facts: [
      { label: 'Starts when', value: 'You approve' },
      { label: 'Does', value: 'Only what was proposed' },
      { label: 'Leaves behind', value: 'A record nobody can edit' },
    ],
    signal: {
      status: 'Done',
      detail:
        'The missing record was created after you approved it, and the decision is logged permanently.',
    },
  },
]
