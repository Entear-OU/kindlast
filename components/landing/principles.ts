/**
 * The responsible-AI principles Kindlast holds itself to.
 *
 * These are the widely agreed principles (the UNESCO recommendation and the
 * OECD AI Principles say substantially the same thing, and the EU AI Act
 * encodes much of it). They are reproduced here as commitments about how this
 * product is built, not as a summary of what the regulation requires. A
 * compliance tool that could not meet the standard it measures you against
 * would not be worth running.
 *
 * Each one is paired with what Kindlast actually does about it, because a
 * principle with no mechanism behind it is a poster. Every mechanism named
 * here is real and checkable in the repository.
 */

export type Principle = {
  name: string
  /** The principle itself, stated plainly. */
  statement: string
  /** What we actually do about it. Must be verifiable in the source. */
  mechanism: string
  /** File under `public/icons`, without extension. */
  icon: string
}

export const PRINCIPLES: Principle[] = [
  {
    name: 'Fairness',
    statement:
      'Systems should treat people equitably and work to reduce bias rather than encode it.',
    mechanism:
      'Findings are derived from the regulation and your own answers, never from inferred characteristics of the people in your records. The detectors are deterministic SQL you can read.',
    icon: 'scales',
  },
  {
    name: 'Reliability and safety',
    statement:
      'Technology should perform dependably across its lifecycle without causing harm.',
    mechanism:
      'An LLM drafts each finding and a deterministic critic rejects it if the proposed action is vague or non-imperative, regenerating until it passes. Nothing reaches you unchecked.',
    icon: 'shield',
  },
  {
    name: 'Privacy and security',
    statement:
      'Personal data should be protected by governance and engineering, not by promises.',
    mechanism:
      'Tenant isolation is enforced in the database by row-level security, not only in application code. You can also self-host the whole thing, so your records never leave your infrastructure.',
    icon: 'lock',
  },
  {
    name: 'Transparency and explainability',
    statement:
      'People should understand when they are interacting with AI and how a decision was reached.',
    mechanism:
      'Every finding cites the specific article it rests on and links the source text. The prompts, the critic rules and the detectors are all in a public repository under AGPL-3.0.',
    icon: 'watch',
  },
  {
    name: 'Inclusiveness',
    statement:
      'Tools should widen access rather than concentrate it among those who can already afford help.',
    mechanism:
      'The people this is for cannot hire a DPO. Making the whole system readable and self-hostable means a company with no budget gets the same engine as one with a legal team.',
    icon: 'network',
  },
  {
    name: 'Accountability',
    statement:
      'Humans remain responsible for the outcomes and governance of an AI system.',
    mechanism:
      'The Executor never acts on its own. Every irreversible step waits on an explicit human approval and writes an immutable audit row with a timestamp and an actor.',
    icon: 'approve',
  },
]
