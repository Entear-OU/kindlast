'use server'

import {
  askAboutFinding,
  type Answer,
  type Failure,
} from '@/lib/agents/conversation'
import { getFinding, listFindings } from '@/lib/findings/client'
import type { KindyState, KindySubject } from '@/components/console/kindy-state'
import { resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * How many findings Kindy offers when it has to ask which one (ENT-284).
 *
 * Five, because the panel is a column beside the page and a list longer than
 * that stops being a choice and becomes a second feed. Somebody with more
 * than five open findings in mind is on the feed already, and asking from
 * the finding they opened is the path this whole change exists to make work.
 */
const CHOICES = 5

/**
 * A message to Kindy, answered in the panel.
 *
 * Kindy has no conversation surface of its own, deliberately: the backend's
 * one conversational RPC is AskAboutFinding, because a finding names exactly
 * one obligation and that is what lets a citation to anything else be
 * refused. A freeform chat would have no subject to check an answer against,
 * which is the failure the whole conversation feature was built to prevent.
 *
 * # THE SUBJECT COMES FROM THE READER, NEVER FROM RECENCY (ENT-284)
 *
 * This used to read the newest pending finding and answer about that,
 * whatever the person had open. On an organisation with three pending
 * findings, opening the DPO one and asking "why does this apply to us?"
 * returned an answer about the ROPA gap, because the ROPA gap was raised
 * later. Nothing about the reply said so beyond a link underneath it.
 *
 * That is the worst shape a wrong answer can take in this product. It is
 * correct, it cites real regulation, and it survives being checked against
 * the article it names, so the one defence a reader has does not fire. The
 * only wrong thing about it is the finding it is about, and recency chose
 * that. Kindy is about to be the only way to ask an agent anything (ENT-286),
 * so a guess would become the normal case rather than an edge one.
 *
 * So the composer posts the finding it was rendered beside, and this asks
 * about that one. The title shown to the reader is read back out of the
 * organisation's own rows rather than taken from the form: a title posted by
 * a browser is a title anybody can edit, and a mislabelled subject is this
 * same bug wearing a disguise. Reading it also means a finding id that is not
 * this organisation's is refused here, before a run is spent on it.
 *
 * # WITH NO SUBJECT, IT ASKS RATHER THAN CHOOSES
 *
 * Away from a finding page there is nothing to anchor to, and the choice
 * between "pick the most likely one" and "ask" is the whole issue in
 * miniature. It asks: the open findings come back as an offer, the question
 * is kept so nobody retypes it, and the person picks. A wrong subject then
 * costs a click and is visible before the answer exists, where a wrong guess
 * costs a decision made on the wrong finding.
 *
 * With nothing open, the reply still comes from code rather than a model,
 * because "there is nothing to talk about" is a fact this process already
 * knows and a model call could only make less reliable.
 *
 * Security is the ask action's, restated: the form carries the words, the
 * slug and at most a finding id, never an organisation id, and the slug is
 * re-resolved against the caller's own memberships on every request. Nothing
 * is revalidated (an answer is not stored), and outcomes return rather than
 * throw (a refusal is a sentence to read, not an exception).
 */
export async function askKindy(
  _previous: KindyState,
  form: FormData,
): Promise<KindyState> {
  const slug = String(form.get('slug') ?? '')
  const question = String(form.get('ask') ?? '').trim()
  const subject = String(form.get('findingId') ?? '').trim()

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

  if (!subject) return await offer(session.accessToken, orgId, question)

  // The title from the record, not from the form. This is also what refuses a
  // finding id belonging to somebody else: GetFinding answers alike for a
  // finding that never existed and one in another organisation, so nothing
  // here can tell the two apart, and neither can the reader.
  const found = await getFinding(session.accessToken, orgId, subject)
  if (!found.ok || !found.value.finding) {
    return {
      status: 'error',
      question,
      message:
        !found.ok && found.error.kind === 'unavailable'
          ? 'core-api is unreachable, so nothing was asked.'
          : 'That finding is no longer here.',
    }
  }
  const target = found.value.finding

  const result = await askAboutFinding(
    session.accessToken,
    orgId,
    target.findingId,
    question,
  )

  if (!result.ok) {
    return { status: 'error', question, message: say(result.error) }
  }
  return read(result.value, question, {
    findingId: target.findingId,
    findingTitle: target.detected,
  })
}

/**
 * The open findings, offered rather than chosen between (ENT-284).
 *
 * Still the pending ones only: a finding somebody already approved is not
 * what "what should I be doing" is about, and an offer of twenty settled
 * findings is a list rather than a question.
 */
async function offer(
  accessToken: string,
  orgId: string,
  question: string,
): Promise<KindyState> {
  const open = await listFindings(accessToken, orgId, {
    status: 'pending',
    pageSize: CHOICES,
  })
  if (!open.ok) {
    return {
      status: 'error',
      question,
      message: 'The findings could not be read, so nothing was asked.',
    }
  }

  const choices: KindySubject[] = (open.value.findings ?? []).map((f) => ({
    findingId: f.findingId,
    findingTitle: f.detected,
  }))

  if (choices.length === 0) return { status: 'nothing-open', question }

  return { status: 'choose', question, choices }
}

/** The outcome, in the states the composer draws. The ask panel's mapping. */
function read(
  answer: Answer,
  question: string,
  subject: KindySubject,
): KindyState {
  if (!answer.intelligenceAvailable) return { status: 'unavailable', question }

  switch (answer.outcome) {
    case 'ANSWER_OUTCOME_SUCCEEDED':
      return {
        status: 'answered',
        question,
        answer: answer.answer ?? '',
        ...subject,
      }

    case 'ANSWER_OUTCOME_REFUSED':
      return {
        status: 'refused',
        question,
        reason:
          answer.outcomeDetail ||
          'a guardrail stopped the answer and gave no reason, which is itself worth reporting',
        ...subject,
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
