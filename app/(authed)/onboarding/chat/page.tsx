import type { UIMessage } from 'ai'
import { redirect } from 'next/navigation'

import {
  getOrCreateActiveSession,
  loadTranscript,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'
import { createClient } from '@/lib/supabase/server'

import { OnboardingChat } from '@/components/onboarding/onboarding-chat'

export default async function OnboardingChatPage() {
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
