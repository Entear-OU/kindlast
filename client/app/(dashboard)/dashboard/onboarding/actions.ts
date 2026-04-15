'use server'

import { createClient } from '@/lib/supabase/server'
import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import type { FullProfileData } from '@/lib/schemas/onboarding'
import { assessGDPRCompliance } from '@/lib/ai/assess-gdpr'
import type { BusinessProfile } from '@/lib/types/database'

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
      // Fetch full profile for AI assessment
      const { data: fullProfile } = await supabase
        .from('business_profiles')
        .select()
        .eq('id', profile.id)
        .single()

      if (fullProfile) {
        // Create assessment with processing status
        const { data: assessment } = await supabase
          .from('assessments')
          .insert({
            user_id: user.id,
            profile_id: profile.id,
            type: 'gdpr',
            status: 'processing',
          })
          .select()
          .single()

        if (assessment) {
          // Run AI assessment directly
          const result = await assessGDPRCompliance(fullProfile as BusinessProfile)

          // Update assessment with results
          await supabase
            .from('assessments')
            .update({
              status: 'complete',
              overall_score: result.overall_score,
              risk_level: result.risk_level,
              result: result as unknown as Record<string, unknown>,
            })
            .eq('id', assessment.id)

          // Save findings
          const findings = result.findings.map((f) => ({
            assessment_id: assessment.id,
            user_id: user.id,
            ...f,
          }))

          if (findings.length > 0) {
            await supabase.from('findings').insert(findings)
          }
        }
      }
    } catch {
      // Non-blocking — user can trigger manually from dashboard
    }
  }

  revalidatePath('/dashboard')
  redirect('/dashboard')
}
