'use server'

import { createClient } from '@/lib/supabase/server'
import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import type { FullProfileData } from '@/lib/schemas/onboarding'

export async function saveBusinessProfile(data: FullProfileData) {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()

  if (!user) throw new Error('Unauthorized')

  const { data: profile, error } = await supabase
    .from('business_profiles')
    .upsert(
      { ...data, user_id: user.id },
      { onConflict: 'user_id' }
    )
    .select()
    .single()

  if (error) throw new Error(error.message)

  revalidatePath('/dashboard')
  return profile
}

export async function completeOnboarding() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()

  if (!user) throw new Error('Unauthorized')

  // Fetch the saved profile to get its ID
  const { data: profile } = await supabase
    .from('business_profiles')
    .select('id')
    .eq('user_id', user.id)
    .single()

  if (profile) {
    // Auto-trigger first GDPR assessment
    try {
      const { data: assessment } = await supabase
        .from('assessments')
        .insert({
          user_id: user.id,
          profile_id: profile.id,
          type: 'gdpr',
          status: 'pending',
        })
        .select()
        .single()

      if (assessment) {
        // Trigger assessment processing in the background via API
        // We don't await this — the dashboard will show the pending status
        const appUrl = process.env.NEXT_PUBLIC_APP_URL || 'http://localhost:3000'
        fetch(`${appUrl}/api/assess`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ profileId: profile.id }),
        }).catch(() => {
          // Assessment will be retried via dashboard button if this fails
        })
      }
    } catch {
      // Non-blocking — user can trigger manually from dashboard
    }
  }

  revalidatePath('/dashboard')
  redirect('/dashboard')
}
