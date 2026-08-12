'use server'

import { revalidatePath } from 'next/cache'

import type { ProcessingActivityInput } from '@/lib/records/ropa'
import { createClient } from '@/lib/supabase/server'

/**
 * Server actions for the ROPA register (ENT-70).
 *
 * Both writes delegate to the SECURITY DEFINER RPCs so the audit entry and the
 * Free-tier cap are enforced in the database, atomically with the change. The
 * RPCs derive the actor from auth.uid(), so the user's session — carried by the
 * RLS-scoped server client — is the only authority; we never pass a user id.
 */

export type ActionResult = { ok: true } | { ok: false; error: string }

const ROPA_PATH = '/records/ropa'

export async function addActivity(input: ProcessingActivityInput): Promise<ActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  const { error } = await supabase.rpc('create_processing_activity', {
    p_name: input.name,
    p_purpose: input.purpose,
    p_legal_basis: input.legal_basis,
    p_data_categories: input.data_categories,
    p_recipients: input.recipients,
    p_retention_period: input.retention_period,
  })
  if (error) return { ok: false, error: error.message }

  revalidatePath(ROPA_PATH)
  return { ok: true }
}

export async function editActivity(
  id: string,
  input: ProcessingActivityInput,
): Promise<ActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  const { error } = await supabase.rpc('update_processing_activity', {
    p_id: id,
    p_name: input.name,
    p_purpose: input.purpose,
    p_legal_basis: input.legal_basis,
    p_data_categories: input.data_categories,
    p_recipients: input.recipients,
    p_retention_period: input.retention_period,
  })
  if (error) return { ok: false, error: error.message }

  revalidatePath(ROPA_PATH)
  return { ok: true }
}
