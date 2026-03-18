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

  revalidatePath('/dashboard')
  redirect('/dashboard')
}
