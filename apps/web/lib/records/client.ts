/**
 * The compliance record, from web's side (ENT-200).
 *
 * Shapes mirror `records.proto` rather than being invented here, so a field that
 * moves in the contract breaks a type instead of quietly rendering blank.
 * Everything optional is optional because the wire genuinely omits it: Connect's
 * JSON drops zero values, so an absent `lastReviewedAt` and a system nobody has
 * reviewed are the same thing.
 *
 * Nothing here derives a status. `completeness` and `urgency` arrive computed,
 * and re-deriving either in the browser would put a second implementation of a
 * regulatory threshold in the client, which is the thing the server-side
 * computation exists to prevent.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/** One entry in the Article 30 record. */
export interface ProcessingActivity {
  processingActivityId: string
  name: string
  purpose?: string
  legalBasis?: string
  dataCategories?: string[]
  recipients?: string[]
  retentionPeriod?: string
  /** Set when the Executor created this from an approved finding. */
  sourceFindingId?: string
  /** complete, incomplete, or review_needed. Computed server-side. */
  completeness?: string
  createdAt?: string
  updatedAt?: string
}

/**
 * A plan limit on manually-created records, and usage against it.
 *
 * `limit: 0` means unlimited, which is what a paid plan and a billing-disabled
 * self-hosted deployment both report. Check for zero rather than rendering
 * "3 of 0 used".
 */
export interface ManualQuota {
  used?: number
  limit?: number
}

/** One system in the AI Act register. */
export interface AiSystem {
  aiSystemId: string
  name: string
  vendor?: string
  purpose?: string
  /** unacceptable, high, limited, minimal, or unclassified. */
  riskClassification?: string
  /** missing, in_progress, or complete. */
  documentationStatus?: string
  /** Absent means never reviewed, which is a state worth showing. */
  lastReviewedAt?: string
  sourceFindingId?: string
  createdAt?: string
  updatedAt?: string
}

/** A data-subject request, and the clock on it. */
export interface Dsar {
  dsarId: string
  subjectName?: string
  requestType?: string
  /** open, in_progress, responded, or closed. The recorded workflow state. */
  status?: string
  receivedAt?: string
  responseDueAt?: string
  respondedAt?: string
  handler?: string
  /** overdue, due_soon, on_track, or answered. Computed server-side. */
  urgency?: string
  /** Whole calendar days, negative once the deadline has passed. */
  daysUntilDue?: number
  sourceFindingId?: string
  createdAt?: string
  updatedAt?: string
  /**
   * How many trail entries stand behind this request (ENT-226).
   *
   * Absent means zero, because Connect's JSON drops zero values, and zero is
   * the number worth showing: a request marked responded with nothing behind it
   * is an assertion the register should not present as evidence.
   */
  trailEntryCount?: number
}

/**
 * One step in assembling a response to a data-subject request (ENT-226).
 *
 * Append-only on the server: there is no update or delete call here because
 * there is no RPC and no grant behind one. A correction is another entry.
 */
export interface DsarTrailEntry {
  entryId: string
  dsarId: string
  /** The store that was searched, in the customer's own words. */
  source: string
  /** searched, found, none_found, disclosed, or withheld. */
  action: string
  detail?: string
  /** When it happened in the world. */
  occurredAt?: string
  /** When it entered the record, which is a different fact. */
  recordedAt?: string
  createdBy?: string
  /** The agent run that produced it, when one did. */
  agentRunId?: string
}

export function listProcessingActivities(
  accessToken: string,
  orgId: string,
  options: { pageToken?: string; pageSize?: number } = {},
) {
  return call<{
    processingActivities?: ProcessingActivity[]
    nextPageToken?: string
    manualQuota?: ManualQuota
  }>('kindlast.core.v1.RecordsService/ListProcessingActivities', {
    accessToken,
    orgId,
    body: options,
  })
}

export function getProcessingActivity(
  accessToken: string,
  orgId: string,
  processingActivityId: string,
) {
  return call<{ processingActivity?: ProcessingActivity }>(
    'kindlast.core.v1.RecordsService/GetProcessingActivity',
    { accessToken, orgId, body: { processingActivityId } },
  )
}

export function listAiSystems(
  accessToken: string,
  orgId: string,
  options: { pageToken?: string; pageSize?: number } = {},
) {
  return call<{ aiSystems?: AiSystem[]; nextPageToken?: string }>(
    'kindlast.core.v1.RecordsService/ListAiSystems',
    { accessToken, orgId, body: options },
  )
}

export function getAiSystem(
  accessToken: string,
  orgId: string,
  aiSystemId: string,
) {
  return call<{ aiSystem?: AiSystem }>(
    'kindlast.core.v1.RecordsService/GetAiSystem',
    { accessToken, orgId, body: { aiSystemId } },
  )
}

export function listDsars(
  accessToken: string,
  orgId: string,
  options: { status?: string; pageToken?: string; pageSize?: number } = {},
) {
  return call<{ dsars?: Dsar[]; nextPageToken?: string }>(
    'kindlast.core.v1.RecordsService/ListDsars',
    { accessToken, orgId, body: options },
  )
}

export function getDsar(accessToken: string, orgId: string, dsarId: string) {
  return call<{ dsar?: Dsar }>('kindlast.core.v1.RecordsService/GetDsar', {
    accessToken,
    orgId,
    body: { dsarId },
  })
}

/**
 * The fields a human supplies for an Article 30 entry.
 *
 * Every field is required in the type even though most are optional in the
 * record, because the contract is a full replacement rather than a patch:
 * omitting a field clears it, so a caller that means "leave this alone" has to
 * send the current value. Making them optional here would let a caller wipe a
 * legal basis by forgetting it.
 */
export interface ProcessingActivityFields {
  name: string
  purpose: string
  legalBasis: string
  dataCategories: string[]
  recipients: string[]
  retentionPeriod: string
}

export interface AiSystemFields {
  name: string
  vendor: string
  purpose: string
  riskClassification: string
  documentationStatus: string
}

export function createProcessingActivity(
  accessToken: string,
  orgId: string,
  fields: ProcessingActivityFields,
) {
  return call<{ processingActivity?: ProcessingActivity }>(
    'kindlast.core.v1.RecordsService/CreateProcessingActivity',
    { accessToken, orgId, body: { fields } },
  )
}

export function updateProcessingActivity(
  accessToken: string,
  orgId: string,
  processingActivityId: string,
  fields: ProcessingActivityFields,
) {
  return call<{ processingActivity?: ProcessingActivity }>(
    'kindlast.core.v1.RecordsService/UpdateProcessingActivity',
    { accessToken, orgId, body: { processingActivityId, fields } },
  )
}

/** `reviewed` is required when the classification is `high`. */
export function createAiSystem(
  accessToken: string,
  orgId: string,
  fields: AiSystemFields,
  reviewed: boolean,
) {
  return call<{ aiSystem?: AiSystem }>(
    'kindlast.core.v1.RecordsService/CreateAiSystem',
    { accessToken, orgId, body: { fields, reviewed } },
  )
}

/** `reviewed` is required whenever this changes the classification. */
export function updateAiSystem(
  accessToken: string,
  orgId: string,
  aiSystemId: string,
  fields: AiSystemFields,
  reviewed: boolean,
) {
  return call<{ aiSystem?: AiSystem }>(
    'kindlast.core.v1.RecordsService/UpdateAiSystem',
    { accessToken, orgId, body: { aiSystemId, fields, reviewed } },
  )
}

/**
 * `receivedAt` is an RFC 3339 timestamp, or omitted to mean today.
 *
 * Not optional by accident: the deadline is computed from it, so a caller that
 * forgets it gets today rather than 1970, and a caller that sends a future date
 * is refused rather than granted a longer clock (ENT-224).
 */
export function logDsar(
  accessToken: string,
  orgId: string,
  subjectName: string,
  requestType: string,
  handler: string,
  receivedAt?: string,
) {
  return call<{ dsar?: Dsar }>('kindlast.core.v1.RecordsService/LogDsar', {
    accessToken,
    orgId,
    body: {
      subjectName,
      requestType,
      handler,
      ...(receivedAt ? { receivedAt } : {}),
    },
  })
}

/** `reviewed` must be true. Returns `applied: false` if already answered. */
export function markDsarResponded(
  accessToken: string,
  orgId: string,
  dsarId: string,
  reviewed: boolean,
) {
  return call<{ applied?: boolean; dsar?: Dsar }>(
    'kindlast.core.v1.RecordsService/MarkDsarResponded',
    { accessToken, orgId, body: { dsarId, reviewed } },
  )
}

/**
 * One request's trail, oldest first.
 *
 * Chronological, unlike every other list here, because a trail answers "what
 * did you do" rather than "what is outstanding". The order comes from the
 * server and is by when the work happened, not by when it was typed up.
 */
export function listDsarTrail(
  accessToken: string,
  orgId: string,
  dsarId: string,
  options: { pageToken?: string; pageSize?: number } = {},
) {
  return call<{ entries?: DsarTrailEntry[]; nextPageToken?: string }>(
    'kindlast.core.v1.RecordsService/ListDsarTrail',
    { accessToken, orgId, body: { dsarId, ...options } },
  )
}

/**
 * Appends one step to a request's trail.
 *
 * `occurredAt` is an RFC 3339 timestamp, or omitted to mean now. A future value
 * is refused rather than clamped, for the same reason a future receipt date is:
 * the point of the field is when the search actually happened.
 *
 * `agentRunId` is empty from every caller that exists today. It is in the
 * signature because §26.4's gateway writes through this call, and provenance
 * added afterwards is provenance nobody recorded.
 */
export function addDsarTrailEntry(
  accessToken: string,
  orgId: string,
  dsarId: string,
  entry: {
    source: string
    action: string
    detail?: string
    occurredAt?: string
    agentRunId?: string
  },
) {
  return call<{ entry?: DsarTrailEntry }>(
    'kindlast.core.v1.RecordsService/AddDsarTrailEntry',
    { accessToken, orgId, body: { dsarId, ...entry } },
  )
}
