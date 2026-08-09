'use server'

import { revalidatePath } from 'next/cache'

import { createClient } from '@/lib/supabase/server'
import { recordsActionError } from '@/lib/records/action-errors'
import { isBlank, REQUIRED_FIELD_MESSAGES } from '@/lib/records/required-fields'

/**
 * Server actions for the DSAR Log (ENT-71).
 *
 * Both writes delegate to the SECURITY DEFINER RPCs so the audit entry and the
 * reviewed-approval gate are enforced in the database, atomically with the
 * change. The RPCs derive the actor from auth.uid(), so the user's session —
 * carried by the RLS-scoped server client — is the only authority.
 */

export type ActionResult = { ok: true } | { ok: false; error: string }

const DSAR_PATH = '/records/dsar'

export interface LogDsarInput {
  subject_name: string
  request_type: string
  handler: string
}

export async function logDsar(input: LogDsarInput): Promise<ActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }
  // ENT-168: logging a requester-less DSAR starts a real 30-day Article 12(3)
  // countdown, which then drives deadline alerts for a request nobody made.
  if (isBlank(input.subject_name)) {
    return { ok: false, error: REQUIRED_FIELD_MESSAGES.dsarRequester }
  }

  const { error } = await supabase.rpc('log_dsar', {
    p_subject_name: input.subject_name,
    p_request_type: input.request_type,
    p_handler: input.handler,
  })
  if (error) return { ok: false, error: recordsActionError(error.message, 'logDsar') }

  revalidatePath(DSAR_PATH)
  return { ok: true }
}

/**
 * "Mark as responded" — an Executor write gated on a reviewed approval. The
 * caller surfaces the explicit confirmation; we pass it through as p_reviewed.
 */
export async function markResponded(id: string): Promise<ActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  const { error } = await supabase.rpc('mark_dsar_responded', {
    p_id: id,
    p_reviewed: true,
  })
  if (error) return { ok: false, error: recordsActionError(error.message, 'markResponded') }

  revalidatePath(DSAR_PATH)
  return { ok: true }
}
