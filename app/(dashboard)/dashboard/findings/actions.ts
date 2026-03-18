'use server'

import { createClient } from '@/lib/supabase/server'
import { revalidatePath } from 'next/cache'

export async function toggleFindingResolved(findingId: string, resolved: boolean) {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) {
    throw new Error('Unauthorized')
  }

  const updateData: Record<string, unknown> = {
    is_resolved: resolved,
    resolved_at: resolved ? new Date().toISOString() : null,
  }

  const { error } = await supabase
    .from('findings')
    .update(updateData)
    .eq('id', findingId)
    .eq('user_id', user.id)

  if (error) {
    throw new Error('Failed to update finding')
  }

  revalidatePath('/dashboard/findings')
  revalidatePath('/dashboard')
}

export async function rerunAssessment(profileId: string) {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) {
    throw new Error('Unauthorized')
  }

  const response = await fetch(`${process.env.NEXT_PUBLIC_APP_URL || 'http://localhost:3000'}/api/assess`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ profileId }),
  })

  if (!response.ok) {
    throw new Error('Failed to start assessment')
  }

  const data = await response.json()

  revalidatePath('/dashboard')
  revalidatePath('/dashboard/findings')

  return data.assessmentId
}
