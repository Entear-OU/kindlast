'use server'

import { revalidatePath } from 'next/cache'

import { orgPath, resolveOrg } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'
import type { RecordActionState } from '@/lib/records/action-state'
import {
  addDsarTrailEntry,
  createAiSystem,
  createProcessingActivity,
  logDsar,
  markDsarResponded,
  updateAiSystem,
  updateProcessingActivity,
  type Failure,
} from '@/lib/records/client'

/**
 * Writing to the compliance record, from web's side (ENT-200).
 *
 * Every action re-resolves the organisation from the slug rather than trusting
 * an id posted by the form, which is the whole security story of this file. A
 * hidden field carrying an org id is a field an attacker can edit; the slug in
 * the URL is resolved against the caller's own memberships, and core-api
 * verifies the resulting header again.
 *
 * Note what is NOT posted: who is making the change. The acting user comes from
 * the session GUC core-api sets from the verified token, so there is no field
 * for a form to carry and nothing for anyone to tamper with.
 *
 * These return a message rather than throwing. A refused write is not an
 * exception, it is a sentence to read, and Next's error boundary would replace
 * the form somebody was filling in with an apology and lose what they typed.
 */

/** Turns a Failure into something worth showing a person. */
function say(error: Failure): RecordActionState {
  switch (error.kind) {
    case 'denied':
      return {
        status: 'error',
        message:
          'Your session is not permitted to change the compliance record. An owner can grant the records write scopes.',
      }
    case 'payment':
      // Deliberately different words from `denied`. One sends you to a
      // colleague, the other to billing, and telling them apart is the whole
      // reason the cap uses its own code.
      return {
        status: 'error',
        message:
          'Your plan limits how many entries you can add by hand. Entries created from approved findings do not count towards it.',
      }
    case 'missing':
      return { status: 'error', message: 'That record is no longer here.' }
    case 'refused':
      // `failed_precondition` is both gates: the reviewed approval and the
      // missing compliance profile. The server's own sentence is shown, because
      // it is the one that says which.
      return {
        status: 'error',
        message: error.message,
        needsReview: needsReview(error.message),
      }
    default:
      return {
        status: 'error',
        message:
          'The change could not be saved just now. This is usually temporary; trying again is worth it.',
      }
  }
}

/**
 * Whether a refusal is the reviewed-approval gate rather than the other
 * precondition.
 *
 * Both arrive as `failed_precondition`, deliberately: they mean the same thing
 * to a caller (do something else first, then retry) and differ only in what that
 * something is. A separate code for each would put product copy in the status
 * code.
 *
 * So this reads the message, which is a string match and therefore the weak
 * point. It is a fallback rather than the mechanism: the forms already show the
 * confirmation whenever a classification changes, because they know the old
 * value and the new one. This only catches the case where the server refused
 * anyway, and getting it wrong costs a person one confusing sentence rather
 * than a broken flow.
 */
function needsReview(message: string): boolean {
  return message.toLowerCase().includes('reviewed approval')
}

/**
 * Resolves the caller's organisation, or the reason it could not.
 *
 * A discriminated union on `ok` rather than a bag with an optional `failed`,
 * so a caller that forgets to check does not compile. The alternative narrowed
 * badly and let `undefined` reach the return type of every action.
 */
type Resolved =
  | { ok: true; accessToken: string; orgId: string }
  | { ok: false; failed: RecordActionState }

async function resolve(slug: string): Promise<Resolved> {
  const session = await currentSession()
  if (!session) {
    return {
      ok: false,
      failed: {
        status: 'error',
        message: 'Your session has expired. Sign in again to continue.',
      },
    }
  }

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status !== 'ok') {
    return {
      ok: false,
      failed: {
        status: 'error',
        message: 'That organisation is not available to you.',
      },
    }
  }

  return {
    ok: true,
    accessToken: session.accessToken,
    orgId: resolved.membership.orgId,
  }
}

/**
 * Splits a comma-separated field into the list the contract wants.
 *
 * Held as raw text while somebody is typing and split only on submit, because
 * splitting on every keystroke makes a half-typed "name, ba" into two entries
 * and moves the cursor. Blank segments are dropped, so a trailing comma is not
 * an empty data category in an Article 30 record.
 */
function list(value: FormDataEntryValue | null): string[] {
  return String(value ?? '')
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '')
}

function text(value: FormDataEntryValue | null): string {
  return String(value ?? '').trim()
}

export async function addProcessingActivity(
  slug: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const name = text(form.get('name'))
  if (name === '') {
    // Refused here rather than at the database, which would substitute
    // "Untitled activity" and leave somebody wondering what they had named it.
    return { status: 'error', message: 'Give the activity a name.' }
  }

  const result = await createProcessingActivity(
    resolved.accessToken,
    resolved.orgId,
    {
      name,
      purpose: text(form.get('purpose')),
      legalBasis: text(form.get('legalBasis')),
      dataCategories: list(form.get('dataCategories')),
      recipients: list(form.get('recipients')),
      retentionPeriod: text(form.get('retentionPeriod')),
    },
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/records'))
  return { status: 'ok', message: `Added ${name}.` }
}

export async function editProcessingActivity(
  slug: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const id = text(form.get('processingActivityId'))
  const name = text(form.get('name'))
  if (name === '') {
    return { status: 'error', message: 'Give the activity a name.' }
  }

  const result = await updateProcessingActivity(
    resolved.accessToken,
    resolved.orgId,
    id,
    {
      name,
      purpose: text(form.get('purpose')),
      legalBasis: text(form.get('legalBasis')),
      dataCategories: list(form.get('dataCategories')),
      recipients: list(form.get('recipients')),
      retentionPeriod: text(form.get('retentionPeriod')),
    },
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/records'))
  return { status: 'ok', message: 'Saved.' }
}

export async function addAiSystem(
  slug: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const name = text(form.get('name'))
  if (name === '') {
    return { status: 'error', message: 'Give the system a name.' }
  }

  const result = await createAiSystem(
    resolved.accessToken,
    resolved.orgId,
    {
      name,
      vendor: text(form.get('vendor')),
      purpose: text(form.get('purpose')),
      riskClassification: text(form.get('riskClassification')),
      documentationStatus: text(form.get('documentationStatus')),
    },
    form.get('reviewed') === 'on',
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/records/ai-systems'))
  return { status: 'ok', message: `Registered ${name}.` }
}

export async function editAiSystem(
  slug: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const id = text(form.get('aiSystemId'))
  const name = text(form.get('name'))
  if (name === '') {
    return { status: 'error', message: 'Give the system a name.' }
  }

  const result = await updateAiSystem(
    resolved.accessToken,
    resolved.orgId,
    id,
    {
      name,
      vendor: text(form.get('vendor')),
      purpose: text(form.get('purpose')),
      riskClassification: text(form.get('riskClassification')),
      documentationStatus: text(form.get('documentationStatus')),
    },
    form.get('reviewed') === 'on',
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/records/ai-systems'))
  return { status: 'ok', message: 'Saved.' }
}

export async function addDsar(
  slug: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const requestType = text(form.get('requestType'))
  if (requestType === '') {
    return { status: 'error', message: 'Say what was asked for.' }
  }

  // A date input posts `YYYY-MM-DD`, which is not what the contract wants and
  // is not a timestamp. Turned into one here rather than in the browser,
  // because the browser's timezone would decide which day it meant.
  //
  // Interpreted as midnight UTC on that date, which can be a few hours before
  // the request truly arrived and never after: erring earlier shortens the
  // organisation's own deadline, and that is the safe direction for a statutory
  // clock. Erring later would hand them time Article 12(3) does not give.
  const receivedOn = text(form.get('receivedAt'))
  const receivedAt = receivedOn ? `${receivedOn}T00:00:00Z` : undefined

  const result = await logDsar(
    resolved.accessToken,
    resolved.orgId,
    text(form.get('subjectName')),
    requestType,
    text(form.get('handler')),
    receivedAt,
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/records/dsars'))

  // The deadline is the fact the person who logged it needs, and it was
  // computed from the receipt date rather than from now (ENT-224).
  const due = result.value.dsar?.responseDueAt
  return {
    status: 'ok',
    message: due
      ? `Logged. A response is due by ${new Date(due).toLocaleDateString()}.`
      : 'Logged.',
  }
}

export async function respondToDsar(
  slug: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const result = await markDsarResponded(
    resolved.accessToken,
    resolved.orgId,
    text(form.get('dsarId')),
    form.get('reviewed') === 'on',
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, '/records/dsars'))

  // `applied: false` means it was already answered. Saying so is better than a
  // success message that implies this call set the date: the date on the record
  // is the one from the first time, which is the one a regulator would read.
  return result.value.applied
    ? { status: 'ok', message: 'Recorded as responded.' }
    : {
        status: 'ok',
        message:
          'This request was already recorded as responded, so nothing changed.',
      }
}

/**
 * Appends one step to a request's trail (ENT-226).
 *
 * There is no edit and no delete beside this, and that is deliberate rather
 * than unfinished: an entry is evidence about how a response to a statutory
 * request was assembled, the database refuses an UPDATE with a trigger that
 * binds even the migrator, and the application holds no DELETE grant. A
 * correction is another entry.
 *
 * Both revalidations matter. The trail is on the detail page, and the count
 * that makes `respondedAt` checkable is on the register listing, so writing an
 * entry changes two pages.
 */
export async function addTrailEntry(
  slug: string,
  dsarId: string,
  _previous: RecordActionState,
  form: FormData,
): Promise<RecordActionState> {
  const resolved = await resolve(slug)
  if (!resolved.ok) return resolved.failed

  const source = text(form.get('source'))
  if (source === '') {
    // Refused here rather than at the database, whose constraint name is not a
    // sentence anyone should be shown.
    return { status: 'error', message: 'Name the store that was searched.' }
  }

  // A datetime-local input posts `YYYY-MM-DDTHH:mm` with no zone, so the
  // browser's own offset is the only thing that says which instant it meant.
  // Read as UTC here rather than in the browser, matching how the receipt date
  // is handled: it errs earlier and never later, and for a record of when a
  // search happened, earlier is the honest direction.
  const occurredOn = text(form.get('occurredAt'))
  const occurredAt = occurredOn ? `${occurredOn}:00Z` : undefined

  const result = await addDsarTrailEntry(
    resolved.accessToken,
    resolved.orgId,
    dsarId,
    {
      source,
      action: text(form.get('action')),
      detail: text(form.get('detail')),
      ...(occurredAt ? { occurredAt } : {}),
    },
  )
  if (!result.ok) return say(result.error)

  revalidatePath(orgPath(slug, `/records/dsars/${dsarId}`))
  revalidatePath(orgPath(slug, '/records/dsars'))

  return { status: 'ok', message: `Recorded against ${source}.` }
}
