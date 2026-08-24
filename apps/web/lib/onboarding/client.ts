/**
 * The first conversation, from web's side (ENT-212).
 *
 * # THE BROWSER TALKS TO CORE-API, AND CORE-API IS THE ONLY WRITER
 *
 * There is no path from here to Intelligence, and there is not going to be one.
 * `AGENTS.md` is unambiguous that no user token reaches the model service: its
 * audience is different, its scope is issued only to a machine principal, and
 * it holds neither tenancy GUCs nor a database handle, so it could not check
 * who is asking and could not persist what was said. A browser-direct interview
 * would be a conversation a refresh destroys.
 *
 * So every turn is a round trip to core-api, and every turn is a row before it
 * is a response.
 *
 * # THE VALUES ARE TYPED HERE TOO, AND THE PARSING IS NOT
 *
 * This module sends what the person typed, verbatim, and core-api decides what
 * it means. That is deliberate: a client that parsed "Ireland, Spain" into a
 * list would be a second implementation of the rule, and the day the two
 * disagree is the day a profile holds something nobody typed. The console
 * renders the control that fits the shape core-api declares, and nothing more.
 */
import { call } from '@/lib/core-api/call'
import type { FactValue, ProfileFactKey } from '@/lib/memory/client'

export type { Failure, Result } from '@/lib/core-api/call'

/** What shape an answer has to take. Decided by core-api, rendered here. */
export type AnswerShape =
  | 'ANSWER_SHAPE_UNSPECIFIED'
  | 'ANSWER_SHAPE_TEXT'
  | 'ANSWER_SHAPE_LIST'
  | 'ANSWER_SHAPE_TRI_STATE'
  | 'ANSWER_SHAPE_NUMBER'

/**
 * One answer a list question offers (ENT-254).
 *
 * Declared by core-api and rendered here, never the other way round. A console
 * that offered a token of its own would produce an answer the server refuses,
 * which is the safe direction for that disagreement to fail in: the alternative
 * is a fact the applicability rules will silently never match.
 */
export interface QuestionOption {
  value?: string
  label?: string
  /** Picking this clears every other choice. */
  exclusive?: boolean
}

export interface Question {
  key?: ProfileFactKey
  prompt?: string
  shape?: AnswerShape
  choices?: string[]
  help?: string
  /** For a list question, the closed set to pick from. Empty for a tri-state. */
  options?: QuestionOption[]
  /**
   * The corpus obligation to quote when somebody asks why we want to know.
   *
   * A slug, and the console renders that obligation's `summary` unedited. The
   * statement of law comes from the corpus row byte for byte (ENT-248), so
   * nothing on this side ever writes one.
   */
  basis?: string
}

export interface OnboardingTurn {
  id?: string
  /** `user` or `assistant`. */
  role?: string
  content?: string
  key?: ProfileFactKey
  value?: FactValue
  /** They declined the question. Different from never having been asked. */
  skipped?: boolean
  createdAt?: string
  createdBy?: string
}

export interface DraftFact {
  key?: ProfileFactKey
  value?: FactValue
  /** Verbatim, what produced the value. Shown beside it so it is checkable. */
  answer?: string
}

export interface OnboardingState {
  sessionId?: string
  status?: string
  transcript?: OnboardingTurn[]
  nextQuestion?: Question
  draft?: DraftFact[]
  readyToConfirm?: boolean
  /**
   * Whether this organisation has a compliance profile at all.
   *
   * The question the console asks before deciding whether to route somebody
   * into onboarding, and deliberately about the profile rather than about a
   * completed session: what decides whether the console has anything to show is
   * the profile.
   */
  profileExists?: boolean
  totalQuestions?: number
  answeredQuestions?: number
}

export function getOnboardingSession(accessToken: string, orgId: string) {
  return call<{ state?: OnboardingState }>(
    'kindlast.core.v1.OnboardingService/GetOnboardingSession',
    { accessToken, orgId },
  )
}

export function startOnboarding(accessToken: string, orgId: string) {
  return call<{ state?: OnboardingState; created?: boolean }>(
    'kindlast.core.v1.OnboardingService/StartOnboarding',
    { accessToken, orgId },
  )
}

export function answerQuestion(
  accessToken: string,
  orgId: string,
  key: ProfileFactKey,
  answer: string,
  skip = false,
) {
  return call<{ state?: OnboardingState }>(
    'kindlast.core.v1.OnboardingService/AnswerQuestion',
    { accessToken, orgId, body: { key, answer, skip } },
  )
}

export function confirmProfile(accessToken: string, orgId: string) {
  return call<{ state?: OnboardingState; profileId?: string }>(
    'kindlast.core.v1.OnboardingService/ConfirmProfile',
    { accessToken, orgId },
  )
}

/**
 * Whether this organisation has been set up enough for the console to mean
 * anything.
 *
 * Returns true when the answer cannot be established, and that direction is the
 * whole point. If core-api is unreachable, bouncing a signed-in person into
 * onboarding would tell them their organisation had been reset during an
 * outage. Assuming they are set up leaves them looking at a page that says the
 * workspace is unavailable, which is true and recoverable.
 *
 * A SUCCESSFUL CALL WITH THE FLAG ABSENT MEANS FALSE, and that is not the same
 * case. Connect's JSON omits a proto3 field at its zero value, so an
 * organisation with no profile answers `{"state":{}}` and there is no flag to
 * read. Only the call itself failing is treated as unknown.
 */
export async function hasComplianceProfile(
  accessToken: string,
  orgId: string,
): Promise<boolean> {
  const result = await getOnboardingSession(accessToken, orgId)
  if (!result.ok) return true
  return result.value.state?.profileExists === true
}
