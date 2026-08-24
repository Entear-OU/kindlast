import type { LeftForYou, PreparedField } from '@/lib/agents/approval'

/**
 * What came back from asking the Hands what approving will do (ENT-278).
 *
 * # FIVE STATES, FOR THE REASONS `AskState` HAS FIVE
 *
 * A REFUSAL IS NOT AN ERROR. §26.3 makes refusal what a working guardrail
 * produces: a tool outside the allow-list, a column the register does not have,
 * a value with no fact behind it, a budget spent. Drawing that as a fault would
 * report the guardrail firing as the guardrail breaking, directly above the
 * button that writes to a customer's compliance record.
 *
 * A DEPLOYMENT WITH NO MODEL IS NOT A REFUSAL EITHER. Intelligence is an
 * optional compose profile, so "this stack has no model" is a true sentence
 * about the deployment rather than anything about this finding.
 *
 * AND A REFUSAL CARRIES A RUN WHERE AN ERROR DOES NOT. A refused run was still
 * a run, recorded, with an id, and showing it is what makes the refusal
 * checkable rather than a sentence to take on faith.
 *
 * # WHY THE PLAN IS TWO LISTS AND NOT ONE
 *
 * `prepared` and `leftForYou` are carried together everywhere, in the proto, in
 * core-api and here. A state that could hold one without the other is a state
 * where a console can draw a record that reads as complete, which is the
 * failure this agent exists to fix: an approved ROPA finding already produces a
 * row saying "Not recorded" in every column, and a half-filled row presented as
 * finished would be worse than that.
 */
export type ExplainState =
  | { status: 'idle' }
  | {
      status: 'explained'
      /** The register in words core-api wrote. Rendered apart from the prose. */
      registerLabel?: string
      explanation: string
      prepared: PreparedField[]
      leftForYou: LeftForYou[]
      agentRunId?: string
    }
  | { status: 'refused'; reason: string; agentRunId?: string }
  | { status: 'unavailable' }
  | { status: 'error'; message: string }

export const idle: ExplainState = { status: 'idle' }
