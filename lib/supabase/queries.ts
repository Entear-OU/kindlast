import type { SupabaseClient } from '@supabase/supabase-js'

export async function getBusinessProfile(supabase: SupabaseClient, userId: string) {
  return supabase
    .from('business_profiles')
    .select('*')
    .eq('user_id', userId)
    .maybeSingle()
}

export async function getLatestAssessment(supabase: SupabaseClient, userId: string) {
  return supabase
    .from('assessments')
    .select('*')
    .eq('user_id', userId)
    .order('created_at', { ascending: false })
    .limit(1)
    .maybeSingle()
}

export async function getFindings(supabase: SupabaseClient, assessmentId: string) {
  return supabase
    .from('findings')
    .select('*')
    .eq('assessment_id', assessmentId)
    .order('severity', { ascending: true })
}

export async function getSubscription(supabase: SupabaseClient, userId: string) {
  return supabase
    .from('subscriptions')
    .select('*')
    .eq('user_id', userId)
    .maybeSingle()
}
