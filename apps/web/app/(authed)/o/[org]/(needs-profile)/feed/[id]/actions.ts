'use server'

import {
  askAboutFinding,
  type Answer,
  type Failure,
} from '@/lib/agents/conversation'
import type { AskState } from '@/lib/agents/ask-state'
import { resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * Asking the Analyst, from web's side (ENT-270).
 *
 * # THE FORM SAYS WHAT TO ASK ABOUT, NEVER WHOSE
 *
 * The same security story the act path has, and it is worth restating because
 * this is the second file in the console to depend on it. The organisation is
 * re-resolved from the slug in the URL against the caller's own memberships;
 * there is no org id field because a hidden field carrying one is a field an
 * attacker can edit. core-api then verifies the resulting header again, and RLS
 * refuses the row regardless. The form supplies which finding, never which
 * tenant.
 *
 * # NOTHING IS REVALIDATED
 *
 * Unlike approve, reject and defer, this changes nothing a page renders. An
 * answer is not stored, the finding is untouched, and the only thing written is
 * the `agent_runs` row Intelligence records. Calling `revalidatePath` would
 * refetch the whole finding to show a paragraph that lives in component state.
 *
 * # AND IT RETURNS RATHER THAN THROWS
 *
 * A refused answer is not an exception, it is a sentence to read, and Next's
 * error boundary would replace the finding somebody was reading with an
 * apology. That matters more here than on the act path: a refusal is the
 * guardrail working, and the panel draws it as such.
 */
export async function ask(
  _previous: AskState,
  form: FormData,
): Promise<AskState> {
  const slug = String(form.get('slug') ?? '')
  const findingId = String(form.get('findingId') ?? '')
  const question = String(form.get('question') ?? '').trim()

  if (!question) {
    return { status: 'error', message: 'There was no question to ask.' }
  }

  const session = await currentSession()
  if (!session) {
    return {
      status: 'error',
      message: 'Your session has expired. Sign in and ask again.',
    }
  }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') {
    return {
      status: 'error',
      message:
        resolved.status === 'not-a-member'
          ? 'You are not a member of this organisation.'
          : 'core-api is unreachable, so nothing was asked.',
    }
  }

  const result = await askAboutFinding(
    session.accessToken,
    resolved.membership.orgId,
    findingId,
    question,
  )

  if (!result.ok) return { status: 'error', message: say(result.error) }
  return read(result.value, question)
}

/**
 * The outcome, as one of the states the panel draws.
 *
 * THE THREE OUTCOMES ARE NOT COLLAPSED, and that is the whole reason this
 * function exists rather than the panel switching on a string. A refusal is
 * what a working guardrail produces (§26.3); a failure is the model being
 * unreachable or answering something that is not the contract; a deployment
 * with no model is a fact about the stack. Rendering all three as "sorry"
 * would report the product's most important behaviour as a fault.
 */
function read(answer: Answer, question: string): AskState {
  if (!answer.intelligenceAvailable) return { status: 'unavailable' }

  switch (answer.outcome) {
    case 'ANSWER_OUTCOME_SUCCEEDED':
      return {
        status: 'answered',
        question,
        answer: answer.answer ?? '',
        run: answer.run,
      }

    case 'ANSWER_OUTCOME_REFUSED':
      return {
        status: 'refused',
        question,
        // The fallback is not decoration. A refusal with no reason leaves the
        // person who asked nothing to read, and "it said no" is the answer that
        // makes a product feel arbitrary.
        reason:
          answer.outcomeDetail ||
          'a guardrail stopped the answer and gave no reason, which is itself worth reporting',
        run: answer.run,
      }

    default:
      // FAILED, or an outcome this build has no word for. Neither is a
      // guardrail firing, so neither is drawn as one.
      return {
        status: 'error',
        message:
          answer.outcomeDetail ||
          'The Analyst could not answer just now. Nothing about this finding changed.',
      }
  }
}

/** Turns a Failure into something worth showing a person. */
function say(error: Failure): string {
  switch (error.kind) {
    case 'denied':
      return 'Your session is not authorised to ask the agents anything.'
    case 'payment':
      return 'Asking the agents needs the Pro plan.'
    case 'missing':
      return 'That finding is no longer here.'
    case 'refused':
      // The reason came from core-api and is written for a person: a finding
      // that cites no obligation has nothing the Analyst could answer from.
      return error.message
    default:
      return 'core-api is unreachable, so nothing was asked.'
  }
}
