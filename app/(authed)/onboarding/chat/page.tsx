import { type UIMessage } from 'ai'
import { redirect } from 'next/navigation'

import {
  getOrCreateActiveSession,
  loadComplianceProfile,
  loadTranscript,
  profileFromRow,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'
import { computePostureSummary, type PostureSummary } from '@/lib/onboarding/posture-summary'
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

  // ENT-154: the opening question used to be generated here with a blocking
  // `generateText` (2–50s) before the page could return any HTML. During a
  // client-side navigation into the chat that left the segment rendering empty
  // and never recovering — a blank conversation until a hard reload. We now
  // render immediately and let the client stream the opening question in via
  // `/api/onboarding/chat` (`{ begin: true }`) when the transcript is empty.

  // ENT-46: hydrate the posture card from the persisted compliance profile
  // on reload. The tool-result `parts` aren't stored in `onboarding_messages`
  // (we only persist text turns), so without this lookup a refresh after
  // completion would drop the card.
  const profileRow = await loadComplianceProfile(supabase, sessionId)
  const initialSummary: PostureSummary | null = profileRow
    ? computePostureSummary(profileFromRow(profileRow))
    : null

  const initialMessages: UIMessage[] = rows.map(uiMessageFromRow)
  return (
    <OnboardingChat
      sessionId={sessionId}
      initialMessages={initialMessages}
      initialSummary={initialSummary}
      startOpening={rows.length === 0}
    />
  )
}
