import { openai } from '@ai-sdk/openai'
import { generateText, type UIMessage } from 'ai'
import { redirect } from 'next/navigation'

import {
  appendMessages,
  getOrCreateActiveSession,
  loadComplianceProfile,
  loadTranscript,
  profileFromRow,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'
import { computePostureSummary, type PostureSummary } from '@/lib/onboarding/posture-summary'
import { ONBOARDING_SYSTEM_PROMPT } from '@/lib/onboarding/system-prompt'
import { createClient } from '@/lib/supabase/server'

import { OnboardingChat } from '@/components/onboarding/onboarding-chat'

const MODEL_ID = process.env.ONBOARDING_CHAT_MODEL ?? 'gpt-5.4-mini'

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

  if (rows.length === 0) {
    const { text } = await generateText({
      model: openai(MODEL_ID),
      system: ONBOARDING_SYSTEM_PROMPT,
      prompt: 'Begin the onboarding interview.',
    })

    await appendMessages(supabase, {
      sessionId,
      userId: user.id,
      messages: [{ role: 'assistant', content: text }],
    })

    const newRows = await loadTranscript(supabase, sessionId)
    const initialMessages: UIMessage[] = newRows.map(uiMessageFromRow)
    return (
      <OnboardingChat
        sessionId={sessionId}
        initialMessages={initialMessages}
        initialSummary={null}
      />
    )
  }

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
    />
  )
}
