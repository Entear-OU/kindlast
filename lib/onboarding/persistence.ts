import type { SupabaseClient } from '@supabase/supabase-js'
import type { UIMessage } from 'ai'

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

/** Hydrate a transcript row back into an AI SDK `UIMessage` for `initialMessages`. */
export function uiMessageFromRow(row: OnboardingMessageRow): UIMessage {
  return {
    id: row.id,
    role: row.role,
    parts: [{ type: 'text', text: row.content }],
  } as UIMessage
}
