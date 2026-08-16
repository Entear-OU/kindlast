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
