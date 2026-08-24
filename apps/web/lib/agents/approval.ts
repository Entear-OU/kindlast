/**
 * Asking the Hands what approving one finding will do, from web's side
 * (ENT-278).
 *
 * Shapes mirror `approvals.proto` rather than being invented here, so a field
 * that moves breaks a type instead of quietly rendering blank. Everything is
 * optional because the wire genuinely omits it: Connect's JSON drops zero
 * values, so an absent `intelligenceAvailable` and a deployment with no model
 * are the same thing.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/**
 * How a run ended, as Connect's JSON spells an enum: the name, not the number.
 *
 * A refusal is not a kind of failure. It is what a working guardrail produces,
 * and a console drawing it as an error would report the product's most
 * important behaviour as a fault.
 */
export type ExplainOutcome =
  | 'EXPLAIN_OUTCOME_UNSPECIFIED'
  | 'EXPLAIN_OUTCOME_SUCCEEDED'
  | 'EXPLAIN_OUTCOME_REFUSED'
  | 'EXPLAIN_OUTCOME_FAILED'

/** One column the Hands filled, and the fact it filled it from. */
export interface PreparedField {
  name: string
  /** The column in a person's words, authored in core-api and not by the model. */
  label?: string
  values?: string[]
  /**
   * The `org_profile_facts` key behind the value.
   *
   * Shown rather than kept, and that is the whole reason this field is on the
   * wire. A value presented as coming from the organisation's own memory that
   * came from nowhere is a fabrication, and the only way a customer can tell
   * the two apart is if the source is on the screen beside the value.
   */
  fromFact?: string
}

/** One column the Hands did not fill, and why. */
export interface LeftForYou {
  name: string
  label?: string
  why?: string
}

export interface Explanation {
  /** False for a deployment running without the model profile, which is supported. */
  intelligenceAvailable?: boolean
  outcome?: ExplainOutcome
  /** Empty unless the run succeeded. core-api withholds a refused explanation. */
  explanation?: string
  /** Why it did not succeed. Written for the person who asked. */
  outcomeDetail?: string
  /** The register an approval writes to, in a person's words. Authored, not generated. */
  registerLabel?: string
  prepared?: PreparedField[]
  leftForYou?: LeftForYou[]
  /**
   * The `agent_runs` row this produced, as an id.
   *
   * An id and not a summary, unlike the Analyst's answer path: nothing reports
   * the skill, version or model on this call, and assembling them from the
   * console's own catalogue would show a customer a run record nobody observed.
   */
  agentRunId?: string
}

export function explainApproval(
  accessToken: string,
  orgId: string,
  findingId: string,
) {
  return call<Explanation>('kindlast.core.v1.ApprovalService/ExplainApproval', {
    accessToken,
    orgId,
    body: { findingId },
  })
}
