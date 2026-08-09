'use server'

import { revalidatePath } from 'next/cache'

import type { DocumentationStatus, RiskClassification } from '@/lib/records/ai-system'
import { createClient } from '@/lib/supabase/server'
import { recordsActionError } from '@/lib/records/action-errors'
import { isBlank, REQUIRED_FIELD_MESSAGES } from '@/lib/records/required-fields'

/**
 * Server actions for the AI Systems Register (ENT-72).
 *
 * Both writes delegate to the SECURITY DEFINER RPCs so the audit entry and the
 * reviewed-approval gate on classification are enforced in the database. The
 * RPCs derive the actor from auth.uid(); the `reviewed` flag carries the founder's
 * explicit confirmation through from the UI.
 */

export type ActionResult = { ok: true } | { ok: false; error: string }

const PATH = '/records/ai-systems'

export interface AiSystemInput {
  name: string
  vendor: string
  purpose: string
  risk_classification: RiskClassification
  documentation_status: DocumentationStatus
}

export async function addSystem(input: AiSystemInput, reviewed: boolean): Promise<ActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }
  // ENT-168: same guard as the ROPA register, against an "Untitled system" row.
  if (isBlank(input.name)) {
    return { ok: false, error: REQUIRED_FIELD_MESSAGES.systemName }
  }

  const { error } = await supabase.rpc('create_ai_system_manual', {
    p_name: input.name,
    p_vendor: input.vendor,
    p_purpose: input.purpose,
    p_risk_classification: input.risk_classification,
    p_documentation_status: input.documentation_status,
    p_reviewed: reviewed,
  })
  if (error) return { ok: false, error: recordsActionError(error.message, 'addSystem') }

  revalidatePath(PATH)
  return { ok: true }
}

export async function editSystem(
  id: string,
  input: AiSystemInput,
  reviewed: boolean,
): Promise<ActionResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }
  if (isBlank(input.name)) {
    return { ok: false, error: REQUIRED_FIELD_MESSAGES.systemName }
  }

  const { error } = await supabase.rpc('update_ai_system', {
    p_id: id,
    p_name: input.name,
    p_vendor: input.vendor,
    p_purpose: input.purpose,
    p_risk_classification: input.risk_classification,
    p_documentation_status: input.documentation_status,
    p_reviewed: reviewed,
  })
  if (error) return { ok: false, error: recordsActionError(error.message, 'editSystem') }

  revalidatePath(PATH)
  return { ok: true }
}
