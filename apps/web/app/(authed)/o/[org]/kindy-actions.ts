'use server'

import {
  askAboutFinding,
  type Answer,
  type Failure,
} from '@/lib/agents/conversation'
import { listFindings } from '@/lib/findings/client'
import type { KindyState } from '@/components/console/kindy-state'
import { resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * A message to Kindy, answered in the panel.
 *
 * Kindy has no conversation surface of its own, deliberately: the backend's
 * one conversational RPC is AskAboutFinding, because a finding names exactly
 * one obligation and that is what lets a citation to anything else be
 * refused. A freeform chat would have no subject to check an answer against,
 * which is the failure the whole conversation feature was built to prevent.
 *
 * So Kindy picks the subject: the newest finding still needing a decision,
 * named in the reply so the reader knows what the answer is about and can
 * open it, regulation and all. With nothing open, the reply comes from code
 * rather than a model, because "there is nothing to talk about" is a fact
 * this process already knows and a model call could only make less reliable.
 *
 * Security is the ask action's, restated: the form carries the words and the
 * slug, never an organisation id, and the slug is re-resolved against the
 * caller's own memberships on every request. Nothing is revalidated (an
 * answer is not stored), and outcomes return rather than throw (a refusal is
 * a sentence to read, not an exception).
 */
export async function askKindy(
  _previous: KindyState,
  form: FormData,
): Promise<KindyState> {
  const slug = String(form.get('slug') ?? '')
  const question = String(form.get('ask') ?? '').trim()

  if (!question) {
    return {
      status: 'error',
      question,
      message: 'There was no message to send.',
    }
  }

  const session = await currentSession()
  if (!session) {
    return {
      status: 'error',
      question,
      message: 'Your session has expired. Sign in and ask again.',
    }
  }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') {
    return {
      status: 'error',
      question,
      message:
        resolved.status === 'not-a-member'
          ? 'You are not a member of this organisation.'
          : 'core-api is unreachable, so nothing was asked.',
    }
  }

  const orgId = resolved.membership.orgId

  const newest = await listFindings(session.accessToken, orgId, {
    status: 'pending',
    pageSize: 1,
  })
  if (!newest.ok) {
    return {
      status: 'error',
      question,
      message: 'The findings could not be read, so nothing was asked.',
    }
  }

  const target = (newest.value.findings ?? [])[0]
  if (!target) return { status: 'nothing-open', question }

  const result = await askAboutFinding(
    session.accessToken,
    orgId,
    target.findingId,
    question,
  )

  if (!result.ok) {
    return { status: 'error', question, message: say(result.error) }
  }
  return read(result.value, question, target.findingId, target.detected)
}

/** The outcome, in the states the composer draws. The ask panel's mapping. */
function read(
  answer: Answer,
  question: string,
  findingId: string,
  findingTitle: string,
): KindyState {
  if (!answer.intelligenceAvailable) return { status: 'unavailable', question }

  switch (answer.outcome) {
    case 'ANSWER_OUTCOME_SUCCEEDED':
      return {
        status: 'answered',
        question,
        answer: answer.answer ?? '',
        findingId,
        findingTitle,
      }

    case 'ANSWER_OUTCOME_REFUSED':
      return {
        status: 'refused',
        question,
        reason:
          answer.outcomeDetail ||
          'a guardrail stopped the answer and gave no reason, which is itself worth reporting',
        findingId,
        findingTitle,
      }

    default:
      return {
        status: 'error',
        question,
        message:
          answer.outcomeDetail ||
          'Kindy could not answer just now. Nothing changed.',
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
      return error.message
    default:
      return 'core-api is unreachable, so nothing was asked.'
  }
}
