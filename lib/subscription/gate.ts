import type { SupabaseClient } from '@supabase/supabase-js'

export async function checkPremium(
  supabase: SupabaseClient,
  userId: string
): Promise<boolean> {
  const { data } = await supabase
    .from('subscriptions')
    .select('plan, status')
    .eq('user_id', userId)
    .eq('status', 'active')
    .maybeSingle()

  return data?.plan === 'premium' && data?.status === 'active'
}
