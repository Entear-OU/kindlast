import type { AgentRunSummary } from '@/lib/agents/conversation'

/**
 * What came back from asking the Analyst (ENT-270).
 *
 * # FIVE STATES, NOT TWO
 *
 * The tempting shape is `{ answer } | { error }`, and it loses the three
 * distinctions this product exists to keep.
 *
 * A REFUSAL IS NOT AN ERROR. §26.3 makes refusal what a working guardrail
 * produces: the model cited an obligation it was never shown, or stated the
 * law, or the run outlasted its budget. Drawing that as a fault would report
 * the guardrail firing as the guardrail breaking, in front of the one person
 * deciding whether to trust this.
 *
 * A DEPLOYMENT WITH NO MODEL IS NOT A REFUSAL EITHER. Intelligence is an
 * optional compose profile, so "this stack has no model" is a true sentence
 * about the deployment rather than anything about the question, and it has a
 * different thing to do about it.
 *
 * AND A REFUSAL CARRIES A RUN WHERE AN ERROR DOES NOT. A refused run was still
 * a run, recorded, with a skill and a model and an id, and showing that is what
 * makes "we tried and it cited something we never gave it" checkable rather
 * than a sentence somebody has to take on faith.
 *
 * `question` is carried back on every state that had one, because the textarea
 * is cleared on submit and a person reading an answer should be able to see
 * what they asked.
 */
export type AskState =
  | { status: 'idle' }
  | {
      status: 'answered'
      question: string
      answer: string
      run?: AgentRunSummary
    }
  | {
      status: 'refused'
      question: string
      reason: string
      run?: AgentRunSummary
    }
  | { status: 'unavailable' }
  | { status: 'error'; message: string }

export const idle: AskState = { status: 'idle' }
