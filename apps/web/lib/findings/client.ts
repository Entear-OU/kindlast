/**
 * The feed and the dashboard, from web's side (ENT-203).
 *
 * Shapes mirror the proto rather than being invented here, so a field that
 * moves in `findings.proto` breaks a type instead of quietly rendering blank.
 * Everything optional is optional because the wire genuinely omits it: Connect's
 * JSON drops zero values, so an absent `snoozedUntil` and a finding that is not
 * snoozed are the same thing.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/**
 * The regulatory basis for a finding, exactly as the Analyst recorded it.
 *
 * `label` is the rendered citation and is what a page shows. Do not rebuild it
 * from `celex` and `article`: a second assembler is a second thing that can
 * disagree with the stored record, and the product's whole claim is that a
 * human can check the citation against the law.
 */
export interface Citation {
  obligationSlug?: string
  title?: string
  celex?: string
  kind?: string
  article?: number
  recital?: number
  annex?: string
  paragraph?: string
  label?: string
  url?: string
}

export interface Finding {
  findingId: string
  status: string
  severity: string
  detected: string
  proposedAction?: string
  effortEstimate?: string
  actionType?: string
  citation?: Citation
  createdAt?: string
  snoozedUntil?: string
  approvedBy?: string
  rejectionReason?: string
}

export interface SupportingChunk {
  ordinal?: number
  label?: string
  quotedText?: string
  sourceUrl?: string
}

export interface SeverityCounts {
  critical?: number
  high?: number
  medium?: number
  low?: number
}

export interface PipelineStatus {
  watcherLastRunAt?: string
  profileExists?: boolean
}

export interface Dashboard {
  posture: string
  postureHeadline?: string
  openBySeverity?: SeverityCounts
  openTotal?: number
  pipeline?: PipelineStatus
}

export function listFindings(
  accessToken: string,
  orgId: string,
  options: { status?: string; pageToken?: string; pageSize?: number } = {},
) {
  return call<{ findings?: Finding[]; nextPageToken?: string }>(
    'kindlast.core.v1.FindingsService/ListFindings',
    { accessToken, orgId, body: options },
  )
}

export function getFinding(
  accessToken: string,
  orgId: string,
  findingId: string,
) {
  return call<{ finding?: Finding; supporting?: SupportingChunk[] }>(
    'kindlast.core.v1.FindingsService/GetFinding',
    { accessToken, orgId, body: { findingId } },
  )
}

export function approveFinding(
  accessToken: string,
  orgId: string,
  findingId: string,
  reviewed: boolean,
) {
  return call<{
    applied?: boolean
    createdRecordId?: string
    createdRecordTable?: string
  }>('kindlast.core.v1.FindingsService/ApproveFinding', {
    accessToken,
    orgId,
    body: { findingId, reviewed },
  })
}

export function rejectFinding(
  accessToken: string,
  orgId: string,
  findingId: string,
  reason: string,
) {
  return call<{ applied?: boolean }>(
    'kindlast.core.v1.FindingsService/RejectFinding',
    { accessToken, orgId, body: { findingId, reason } },
  )
}

export function snoozeFinding(
  accessToken: string,
  orgId: string,
  findingId: string,
  days: number,
) {
  return call<{ applied?: boolean; snoozedUntil?: string }>(
    'kindlast.core.v1.FindingsService/SnoozeFinding',
    { accessToken, orgId, body: { findingId, days } },
  )
}

export function getDashboard(accessToken: string, orgId: string) {
  return call<Dashboard>('kindlast.core.v1.DashboardService/GetDashboard', {
    accessToken,
    orgId,
  })
}
