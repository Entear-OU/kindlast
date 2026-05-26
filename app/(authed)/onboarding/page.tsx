import type { UIMessage } from 'ai'
import { redirect } from 'next/navigation'

import {
  getOrCreateActiveSession,
  loadTranscript,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'
import { createClient } from '@/lib/supabase/server'

import { OnboardingChat } from '@/components/onboarding/onboarding-chat'

/**
 * Conversational onboarding entry point (ENT-44).
 *
 * Resume behaviour: server resolves the user's active in-progress session
 * (or creates one) and hydrates its transcript into `initialMessages`. The
 * chat URL is identical for every visit, so a returning founder picks up
 * where they left off without remembering anything.
 */
export default async function OnboardingPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const sessionId = await getOrCreateActiveSession(supabase, user.id)
  const rows = await loadTranscript(supabase, sessionId)
  const initialMessages: UIMessage[] = rows.map(uiMessageFromRow)

  return <OnboardingChat sessionId={sessionId} initialMessages={initialMessages} />
}
