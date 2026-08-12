import type { SupabaseClient } from '@supabase/supabase-js'
import type { UIMessage } from 'ai'

import type { ComplianceProfile } from './extraction'

/**
 * Data-access helpers for the conversational onboarding flow (ENT-44).
 *
 * Every read and write goes through an authenticated Supabase client so the
 * RLS policies from ENT-47 are the source of truth — these helpers never
 * bypass them with the service-role client.
 */

export type OnboardingMessageRow = {
  id: string
  session_id: string
  user_id: string
  role: 'user' | 'assistant'
  content: string
  ordering: number
  created_at: string
}

/**
 * Return the user's active in-progress session, creating one if none exists.
 *
 * Resume support (ENT-44 AC): the same call on every page load picks up the
 * existing session if the user dropped off mid-flow, or starts a fresh one
 * for first-time visitors and post-completed re-interviews.
 */
export async function getOrCreateActiveSession(
  supabase: SupabaseClient,
  userId: string,
): Promise<string> {
  const existing = await supabase
    .from('onboarding_sessions')
    .select('id')
    .eq('user_id', userId)
    .eq('status', 'in_progress')
    .order('started_at', { ascending: false })
    .limit(1)
    .maybeSingle()

  if (existing.error) {
    throw new Error(`getOrCreateActiveSession: select failed: ${existing.error.message}`)
  }
  if (existing.data) {
    return existing.data.id
  }

  const created = await supabase
    .from('onboarding_sessions')
    .insert({ user_id: userId })
    .select('id')
    .single()

  if (created.error || !created.data) {
    throw new Error(
      `getOrCreateActiveSession: insert failed: ${created.error?.message ?? 'no row returned'}`,
    )
  }
  return created.data.id
}

/** Load every message in a session, ordered. */
export async function loadTranscript(
  supabase: SupabaseClient,
  sessionId: string,
): Promise<OnboardingMessageRow[]> {
  const { data, error } = await supabase
    .from('onboarding_messages')
    .select('id, session_id, user_id, role, content, ordering, created_at')
    .eq('session_id', sessionId)
    .order('ordering', { ascending: true })

  if (error) {
    throw new Error(`loadTranscript: ${error.message}`)
  }
  return (data ?? []) as OnboardingMessageRow[]
}

/**
 * Append messages to a session, assigning sequential `ordering` after the
 * current max. Atomic per-call: a single insert with computed indices, so a
 * concurrent writer can at worst collide on the unique constraint and the
 * caller retries.
 */
export async function appendMessages(
  supabase: SupabaseClient,
  args: {
    sessionId: string
    userId: string
    messages: ReadonlyArray<{ role: 'user' | 'assistant'; content: string }>
  },
): Promise<OnboardingMessageRow[]> {
  if (args.messages.length === 0) return []

  const max = await supabase
    .from('onboarding_messages')
    .select('ordering')
    .eq('session_id', args.sessionId)
    .order('ordering', { ascending: false })
    .limit(1)
    .maybeSingle()

  if (max.error) {
    throw new Error(`appendMessages: select max failed: ${max.error.message}`)
  }

  const start = (max.data?.ordering ?? -1) + 1
  const rows = args.messages.map((m, i) => ({
    session_id: args.sessionId,
    user_id: args.userId,
    role: m.role,
    content: m.content,
    ordering: start + i,
  }))

  const { data, error } = await supabase
    .from('onboarding_messages')
    .insert(rows)
    .select('id, session_id, user_id, role, content, ordering, created_at')

  if (error || !data) {
    throw new Error(`appendMessages: insert failed: ${error?.message ?? 'no rows returned'}`)
  }
  return data as OnboardingMessageRow[]
}

/** Flatten an AI SDK `UIMessage` to plain text (concatenated text parts). */
export function textFromUIMessage(message: UIMessage): string {
  return message.parts
    .filter((part) => part.type === 'text')
    .map((part) => (part as { type: 'text'; text: string }).text)
    .join('')
}

/**
 * Project an AI SDK `UIMessage[]` into the `{role, content}` rows the
 * persister accepts, dropping assistant turns whose flattened text is empty
 * or whitespace-only (ENT-87).
 *
 * Why drop assistant turns? When `streamText` errors mid-flight (e.g. a
 * model-side 401 or rate limit), `toUIMessageStreamResponse.onFinish` still
 * fires — but the assistant `UIMessage` arrives with no rendered text parts.
 * Persisting that row poisons every subsequent prompt (the LLM sees `""` as
 * its last assistant turn) and re-renders an empty bubble on resume.
 *
 * User turns are kept verbatim: the founder did interact, and preserving
 * the row lets a retry resume from the same input.
 */
export function messagesToPersist(
  messages: ReadonlyArray<UIMessage>,
): Array<{ role: 'user' | 'assistant'; content: string }> {
  const rows: Array<{ role: 'user' | 'assistant'; content: string }> = []
  for (const message of messages) {
    const role = message.role as 'user' | 'assistant'
    const content = textFromUIMessage(message)
    if (role === 'assistant' && content.trim() === '') continue
    rows.push({ role, content })
  }
  return rows
}

/** Hydrate a transcript row back into an AI SDK `UIMessage` for `initialMessages`. */
export function uiMessageFromRow(row: OnboardingMessageRow): UIMessage {
  return {
    id: row.id,
    role: row.role,
    parts: [{ type: 'text', text: row.content }],
  } as UIMessage
}

export type ComplianceProfileRow = {
  id: string
  session_id: string
  user_id: string
  industry: string
  eu_jurisdictions: string[]
  data_categories: string[]
  data_subjects: string[]
  ai_systems: string[]
  has_dpo: 'yes' | 'no' | 'unsure'
  has_ropa: 'yes' | 'no' | 'unsure'
  transfers_outside_eu: 'yes' | 'no' | 'unsure'
  transfer_destinations: string[]
  vendor_list: string
  staff_count: number | null
  created_at: string
  updated_at: string
}

/**
 * Insert a `compliance_profiles` row for a session (ENT-45).
 *
 * Single insert — atomic per-call. The DB's unique constraint on `session_id`
 * means a duplicate call (e.g. tool fired twice mid-stream) gets rejected
 * rather than producing two profile rows for one interview.
 *
 * Returns the inserted row so callers can include it in a response payload
 * without a follow-up read.
 */
export async function persistComplianceProfile(
  supabase: SupabaseClient,
  args: {
    sessionId: string
    userId: string
    profile: ComplianceProfile
  },
): Promise<ComplianceProfileRow> {
  const row = {
    session_id: args.sessionId,
    user_id: args.userId,
    industry: args.profile.industry,
    eu_jurisdictions: args.profile.euJurisdictions,
    data_categories: args.profile.dataCategories,
    data_subjects: args.profile.dataSubjects,
    ai_systems: args.profile.aiSystems,
    has_dpo: args.profile.hasDpo,
    has_ropa: args.profile.hasRopa,
    transfers_outside_eu: args.profile.transfersOutsideEu,
    transfer_destinations: args.profile.transferDestinations,
    vendor_list: args.profile.vendorList,
    staff_count: args.profile.staffCount,
  }

  const { data, error } = await supabase
    .from('compliance_profiles')
    .insert(row)
    .select(
      'id, session_id, user_id, industry, eu_jurisdictions, data_categories, data_subjects, ai_systems, has_dpo, has_ropa, transfers_outside_eu, transfer_destinations, vendor_list, staff_count, created_at, updated_at',
    )
    .single()

  if (error || !data) {
    throw new Error(
      `persistComplianceProfile: insert failed: ${error?.message ?? 'no row returned'}`,
    )
  }
  return data as ComplianceProfileRow
}

/**
 * Load the compliance profile for a session, or `null` if none exists yet.
 *
 * Used by `app/(authed)/onboarding/chat/page.tsx` (ENT-46) to hydrate the
 * inline posture summary card on page reload — without this, refreshing
 * the chat after completion would drop the card because tool-result parts
 * aren't persisted in `onboarding_messages`.
 */
export async function loadComplianceProfile(
  supabase: SupabaseClient,
  sessionId: string,
): Promise<ComplianceProfileRow | null> {
  const { data, error } = await supabase
    .from('compliance_profiles')
    .select(
      'id, session_id, user_id, industry, eu_jurisdictions, data_categories, data_subjects, ai_systems, has_dpo, has_ropa, transfers_outside_eu, transfer_destinations, vendor_list, staff_count, created_at, updated_at',
    )
    .eq('session_id', sessionId)
    .maybeSingle()

  if (error) {
    throw new Error(`loadComplianceProfile(${sessionId}): ${error.message}`)
  }
  return (data as ComplianceProfileRow | null) ?? null
}

/**
 * Reverse mapping for `loadComplianceProfile` — converts a row's snake_case
 * shape back to the camelCase `ComplianceProfile` the projection consumes.
 */
export function profileFromRow(row: ComplianceProfileRow): ComplianceProfile {
  return {
    industry: row.industry,
    euJurisdictions: row.eu_jurisdictions,
    dataCategories: row.data_categories,
    dataSubjects: row.data_subjects,
    aiSystems: row.ai_systems,
    hasDpo: row.has_dpo,
    hasRopa: row.has_ropa,
    transfersOutsideEu: row.transfers_outside_eu,
    transferDestinations: row.transfer_destinations,
    vendorList: row.vendor_list,
    staffCount: row.staff_count,
  }
}

/**
 * Mark a session completed (ENT-45 — sibling to `persistComplianceProfile`).
 *
 * Separate from the profile insert because Supabase JS has no transaction
 * primitive: if the profile insert succeeds and this call fails, the profile
 * row is still the authoritative signal that onboarding finished (a downstream
 * reader can treat "session has a profile" as equivalent to "completed"). The
 * status flip is best-effort UX — it lets `getOrCreateActiveSession` open a
 * fresh session for the next re-interview without a manual cleanup step.
 */
export async function markSessionCompleted(
  supabase: SupabaseClient,
  sessionId: string,
): Promise<void> {
  const { error } = await supabase
    .from('onboarding_sessions')
    .update({ status: 'completed', completed_at: new Date().toISOString() })
    .eq('id', sessionId)

  if (error) {
    throw new Error(`markSessionCompleted(${sessionId}): ${error.message}`)
  }
}
