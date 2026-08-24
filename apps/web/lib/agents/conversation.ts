/**
 * Asking the Analyst about one finding, from web's side (ENT-270).
 *
 * Shapes mirror `conversation.proto` rather than being invented here, so a
 * field that moves breaks a type instead of quietly rendering blank. Everything
 * is optional because the wire genuinely omits it: Connect's JSON drops zero
 * values, so an absent `intelligenceAvailable` and a deployment with no model
 * are the same thing.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/**
 * How a run ended, as Connect's JSON spells an enum: the name, not the number.
 *
 * The three outcomes are not three kinds of failure. A refusal is what a
 * working guardrail produces, and a console that drew it as an error would be
 * reporting the product's most important behaviour as a fault.
 */
export type AnswerOutcome =
  | 'ANSWER_OUTCOME_UNSPECIFIED'
  | 'ANSWER_OUTCOME_SUCCEEDED'
  | 'ANSWER_OUTCOME_REFUSED'
  | 'ANSWER_OUTCOME_FAILED'

/**
 * The `agent_runs` row behind an answer, as a person reads it.
 *
 * §26 requires that a run leaves a record a customer can read, and this is that
 * record for the exchange somebody just had. It is carried in the response
 * rather than fetched, because `agent_runs` has a write path and no read path;
 * when there is one, this should become the id and a second call.
 */
export interface AgentRunSummary {
  agentRunId?: string
  skill?: string
  skillVersion?: string
  model?: string
  modelVersion?: string
  /** `instance` for the deployment's own model, otherwise the chosen provider. */
  provider?: string
  resolvedCitations?: string[]
}

export interface Answer {
  /** False for a deployment running without the model profile, which is supported. */
  intelligenceAvailable?: boolean
  outcome?: AnswerOutcome
  /** Empty unless the run succeeded. core-api withholds a refused answer. */
  answer?: string
  /** Why it did not succeed. Written for the person who asked. */
  outcomeDetail?: string
  run?: AgentRunSummary
}

/**
 * How long a question may be.
 *
 * A COURTESY, NOT THE CONTROL. `MAX_QUESTION_CHARS` in
 * `apps/intelligence/src/kindlast_intelligence/skills/conversation.py` is what
 * actually refuses one, because a limit that lives in a form is a limit the
 * next caller does not have. This one exists so the textarea stops accepting
 * characters that would earn a refusal, which is a kinder way to learn it than
 * pressing Ask and waiting.
 *
 * Keeping the two in step is a manual job today. It is the same repetition the
 * agent catalogue has, and unlike the catalogue there is no test reading the
 * Python for it, because being generous here is safe: a form that allows more
 * than the harness does produces a recorded refusal, which is the outcome this
 * whole surface is built to render.
 */
export const MAX_QUESTION_CHARS = 1000

export function askAboutFinding(
  accessToken: string,
  orgId: string,
  findingId: string,
  question: string,
) {
  return call<Answer>('kindlast.core.v1.ConversationService/AskAboutFinding', {
    accessToken,
    orgId,
    body: { findingId, question },
  })
}
