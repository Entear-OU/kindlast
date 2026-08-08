import type { SupabaseClient } from '@supabase/supabase-js'

/**
 * Data access + presentation helpers for the DSAR Log (ENT-71).
 *
 * Reads go through an authenticated Supabase client (RLS is the source of
 * truth). The two writes — manual "Log a DSAR" and "Mark as responded" — go
 * through the SECURITY DEFINER RPCs `log_dsar` / `mark_dsar_responded`
 * (migration 20260601170000), called from `./dsar` server actions, so each
 * change records an audit entry and the reviewed-approval gate is enforced.
 */

export type DsarStatusValue = 'open' | 'in_progress' | 'responded' | 'closed'

export interface Dsar {
  id: string
  subject_name: string | null
  request_type: string | null
  handler: string | null
  status: DsarStatusValue
  received_at: string
  response_due_at: string
  responded_at: string | null
  finding_id: string | null
  created_at: string
  updated_at: string
}

export type DsarTone = 'done' | 'danger' | 'warn' | 'info'

export interface DsarStatusBadge {
  label: string
  tone: DsarTone
}

const COLUMNS =
  'id,subject_name,request_type,handler,status,received_at,response_due_at,responded_at,finding_id,created_at,updated_at'

export async function loadDsars(supabase: SupabaseClient, userId: string): Promise<Dsar[]> {
  const { data, error } = await supabase
    .from('dsars')
    .select(COLUMNS)
    .eq('user_id', userId)
    .order('response_due_at', { ascending: true })

  if (error) {
    throw new Error(`loadDsars: ${error.message}`)
  }
  return (data ?? []) as Dsar[]
}

/** Whole-day difference from `now` to the due date (negative = overdue). */
export function daysUntilDue(dsar: Pick<Dsar, 'response_due_at'>, now: Date = new Date()): number {
  const due = new Date(dsar.response_due_at)
  const startOfDay = (d: Date) => Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate())
  return Math.round((startOfDay(due) - startOfDay(now)) / 86_400_000)
}

/** A request is still open until it has been responded to or closed. */
export function isOpenDsar(dsar: Pick<Dsar, 'status'>): boolean {
  return dsar.status === 'open' || dsar.status === 'in_progress'
}

/**
 * The status pill: responded/closed read as done; an open request is overdue,
 * due soon (within the Article 12(3) 10-day escalation window), or simply open.
 */
export function deriveDsarStatus(dsar: Dsar, now: Date = new Date()): DsarStatusBadge {
  if (dsar.status === 'responded') return { label: 'Responded', tone: 'done' }
  if (dsar.status === 'closed') return { label: 'Closed', tone: 'done' }

  const days = daysUntilDue(dsar, now)
  if (days < 0) return { label: 'Overdue', tone: 'danger' }
  if (days <= 10) return { label: 'Due soon', tone: 'warn' }
  return { label: dsar.status === 'in_progress' ? 'In progress' : 'Open', tone: 'info' }
}

/** Human "deadline" cell: a dash once answered, else a countdown / overdue note. */
export function formatDueLabel(dsar: Dsar, now: Date = new Date()): string {
  if (!isOpenDsar(dsar)) return '–'
  const days = daysUntilDue(dsar, now)
  if (days < 0) return `${Math.abs(days)} day${Math.abs(days) === 1 ? '' : 's'} overdue`
  if (days === 0) return 'Due today'
  return `Due in ${days} day${days === 1 ? '' : 's'}`
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** Compact date for the received / response-sent columns, e.g. "8 May". */
export function formatDate(iso: string | null, now: Date = new Date()): string {
  if (!iso) return '–'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '–'
  const base = `${d.getDate()} ${MONTHS[d.getMonth()]}`
  return d.getFullYear() === now.getFullYear() ? base : `${base} ${d.getFullYear()}`
}
