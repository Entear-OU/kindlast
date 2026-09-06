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

/**
 * Whether Kindy is answering right now (ENT-296), as Connect's JSON spells an
 * enum: the name, not the number.
 *
 * THREE STATES AND NOT A BOOLEAN, and the middle one is the reason. A
 * deployment that runs no Intelligence is supported and must not be drawn as
 * an outage, and a deployment whose Intelligence has stopped is an outage and
 * must not be drawn as a working product. A boolean would have to pick which
 * of those two lies to tell.
 */
export type Availability =
  | 'AVAILABILITY_UNSPECIFIED'
  | 'AVAILABILITY_REACHABLE'
  | 'AVAILABILITY_UNREACHABLE'
  | 'AVAILABILITY_NOT_CONFIGURED'

export interface AgentStatus {
  availability?: Availability
  /**
   * When the probe behind this answer ran, RFC 3339 as Connect spells a
   * timestamp. Older than now by up to core-api's cache window, which is the
   * point of carrying it: presence that says how old it is beats presence that
   * implies it is live.
   */
  checkedAt?: string
}

/**
 * Ask core-api whether Kindy would answer.
 *
 * Never throws for an unreachable core-api: `call` returns a Failure, and the
 * rail draws that the same as unreachable. A status read is chrome, and chrome
 * that can fail a page render is worse than no chrome.
 */
export function getAgentStatus(accessToken: string, orgId: string) {
  return call<AgentStatus>(
    'kindlast.core.v1.ConversationService/GetAgentStatus',
    {
      accessToken,
      orgId,
      body: {},
    },
  )
}
