import type { SupabaseClient } from '@supabase/supabase-js'

/**
 * The console's onboarding gate (ENT-166).
 *
 * Every console surface is a view over a compliance profile: the dashboard
 * reads the Watcher's verdict on it, the feed lists findings against it, the
 * records pages write rows that hang off it. A signed-in user who has not
 * finished onboarding has no profile, so all of that is empty, and the pages
 * used to render anyway.
 *
 * That produced two failures. The dashboard told them "your first scan runs
 * within 24 hours", which is false because `run_watcher()` iterates
 * `compliance_profiles` and will never see them. And the ROPA add form let
 * them submit into an RPC that can only raise, surfacing
 * "create_processing_activity: no compliance profile for user" as UI copy.
 *
 * Both are the same missing precondition, so it is checked in one place rather
 * than patched per page.
 */

/** Whether this user has a compliance profile, i.e. has finished onboarding. */
export async function hasComplianceProfile(
  supabase: SupabaseClient,
  userId: string,
): Promise<boolean> {
  const { data, error } = await supabase
    .from('compliance_profiles')
    .select('id')
    .eq('user_id', userId)
    .limit(1)
    .maybeSingle()

  if (error) {
    throw new Error(`hasComplianceProfile: ${error.message}`)
  }

  return data !== null
}
