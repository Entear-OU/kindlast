/**
 * What Kindlast knows about the organisation, from web's side (ENT-228).
 *
 * # THE CORRECTION IS NOT AN UPDATE, AND A CLIENT SHOULD NOT PRETEND IT IS
 *
 * `correctFact` closes the current value and records a new one. There is no
 * PUT, no patch of a resource at a URL, and no way to overwrite a value in
 * place: `kindlast_app` holds `update (valid_to)` and not `update (value)`, so
 * the database refuses it rather than the convention discouraging it.
 *
 * That shapes the UI as much as the API. A form that reads as "edit this
 * field" is describing something the product cannot do. It is closer to
 * "record what is true now", and the previous answer stays visible.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/** The closed set of facts this product understands. Mirrors the proto enum. */
export type ProfileFactKey =
  | 'PROFILE_FACT_KEY_INDUSTRY'
  | 'PROFILE_FACT_KEY_EU_JURISDICTIONS'
  | 'PROFILE_FACT_KEY_DATA_CATEGORIES'
  | 'PROFILE_FACT_KEY_DATA_SUBJECTS'
  | 'PROFILE_FACT_KEY_AI_SYSTEMS'
  | 'PROFILE_FACT_KEY_HAS_DPO'
  | 'PROFILE_FACT_KEY_HAS_ROPA'
  | 'PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU'
  | 'PROFILE_FACT_KEY_TRANSFER_DESTINATIONS'
  | 'PROFILE_FACT_KEY_STAFF_COUNT'
  | 'PROFILE_FACT_KEY_HIGH_RISK_PROCESSING'
  | 'PROFILE_FACT_KEY_HIGH_RISK_AI_SYSTEM'
  | 'PROFILE_FACT_KEY_LARGE_SCALE_MONITORING'
  | 'PROFILE_FACT_KEY_LAWFUL_BASES'

export type TriState =
  | 'TRI_STATE_YES'
  | 'TRI_STATE_NO'
  | 'TRI_STATE_UNSURE'
  | 'TRI_STATE_UNSPECIFIED'

/**
 * One value. Exactly one field is set, and which one depends on the key.
 *
 * `unsure` is a real answer rather than a missing one: "we do not know whether
 * we have a record of processing activities" is a finding in itself, and
 * rendering it as blank would turn it into "we did not ask".
 */
export interface FactValue {
  text?: string
  list?: { values?: string[] }
  number?: string
  triState?: TriState
}

export interface ProfileFact {
  key?: ProfileFactKey
  value?: FactValue
  /** onboarding, integration, human, agent, import. Shown against the value. */
  source?: string
  /** Empty for a fact somebody simply stated, which is most of onboarding. */
  evidenceId?: string
  validFrom?: string
  /** Empty means this is what we believe now. */
  validTo?: string
  recordedBy?: string
}

export interface Evidence {
  id?: string
  source?: string
  kind?: string
  connectionId?: string
  /** When it was true at the source. */
  observedAt?: string
  /** When we learned it. Routinely far from observedAt, and the gap matters. */
  fetchedAt?: string
  bodyJson?: string
  supersededBy?: string
}

export function listProfileFacts(accessToken: string, orgId: string) {
  return call<{ facts?: ProfileFact[] }>(
    'kindlast.core.v1.MemoryService/ListProfileFacts',
    { accessToken, orgId },
  )
}

export function getFactHistory(
  accessToken: string,
  orgId: string,
  key: ProfileFactKey,
) {
  return call<{ facts?: ProfileFact[] }>(
    'kindlast.core.v1.MemoryService/GetFactHistory',
    { accessToken, orgId, body: { key } },
  )
}

export function correctFact(
  accessToken: string,
  orgId: string,
  key: ProfileFactKey,
  value: FactValue,
  note?: string,
) {
  return call<{ fact?: ProfileFact; changed?: boolean }>(
    'kindlast.core.v1.MemoryService/CorrectFact',
    { accessToken, orgId, body: { key, value, note } },
  )
}

export function listEvidence(
  accessToken: string,
  orgId: string,
  pageToken?: string,
) {
  return call<{ evidence?: Evidence[]; nextPageToken?: string }>(
    'kindlast.core.v1.MemoryService/ListEvidence',
    { accessToken, orgId, body: { pageSize: 50, pageToken } },
  )
}

/** How each fact is asked about, in the customer's words rather than ours. */
export const FACT_LABELS: Record<ProfileFactKey, string> = {
  PROFILE_FACT_KEY_INDUSTRY: 'Industry',
  PROFILE_FACT_KEY_EU_JURISDICTIONS: 'EU jurisdictions',
  PROFILE_FACT_KEY_DATA_CATEGORIES: 'Categories of personal data',
  PROFILE_FACT_KEY_DATA_SUBJECTS: 'Whose data you hold',
  PROFILE_FACT_KEY_AI_SYSTEMS: 'AI systems in use',
  PROFILE_FACT_KEY_HAS_DPO: 'Data protection officer appointed',
  PROFILE_FACT_KEY_HAS_ROPA: 'Record of processing activities kept',
  PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU:
    'Transfers personal data outside the EU',
  PROFILE_FACT_KEY_TRANSFER_DESTINATIONS: 'Where data is transferred',
  PROFILE_FACT_KEY_STAFF_COUNT: 'Staff',

  // The four ENT-246 added, and the wording is load-bearing rather than
  // decorative: each one decides whether an obligation applies, so a question
  // a reader answers loosely produces a finding they did not earn or hides one
  // they did. They name the legal test rather than summarising it.
  PROFILE_FACT_KEY_HIGH_RISK_PROCESSING:
    'Processing likely to result in a high risk to people (GDPR Article 35)',
  PROFILE_FACT_KEY_HIGH_RISK_AI_SYSTEM:
    'Provides or deploys a high-risk AI system (AI Act Annex III)',
  PROFILE_FACT_KEY_LARGE_SCALE_MONITORING:
    'Monitors data subjects regularly and systematically, on a large scale',
  PROFILE_FACT_KEY_LAWFUL_BASES: 'Lawful bases relied on',
}

/** Where a value came from, said plainly. */
export const SOURCE_LABELS: Record<string, string> = {
  onboarding: 'You told us during setup',
  integration: 'Read from a connected tool',
  human: 'Someone in your team recorded it',
  agent: 'Worked out by Kindlast',
  import: 'Imported',
}

/**
 * Render a value for reading.
 *
 * Returns null when there is genuinely nothing, so a caller can say "not
 * recorded" rather than printing an empty string. That distinction is the
 * whole reason `unsure` exists as a value: not recorded and recorded as
 * unknown are different states, and a product that renders both as blank has
 * thrown away the one that is actionable.
 */
export function readValue(fact: ProfileFact): string | null {
  const value = fact.value
  if (!value) return null

  if (typeof value.text === 'string') return value.text || null
  if (typeof value.number === 'string') return value.number
  if (value.list) {
    const values = value.list.values ?? []
    // An empty list is an answer: "we operate no AI systems" is what somebody
    // said, not something they skipped.
    return values.length > 0 ? values.join(', ') : 'None'
  }
  switch (value.triState) {
    case 'TRI_STATE_YES':
      return 'Yes'
    case 'TRI_STATE_NO':
      return 'No'
    case 'TRI_STATE_UNSURE':
      return 'Not sure'
    default:
      return null
  }
}
