import { openai } from '@ai-sdk/openai'
import {
  convertToModelMessages,
  createIdGenerator,
  streamText,
  type UIMessage,
} from 'ai'
import { NextResponse } from 'next/server'

import {
  appendMessages,
  getOrCreateActiveSession,
  loadTranscript,
  messagesToPersist,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'
import { ONBOARDING_SYSTEM_PROMPT } from '@/lib/onboarding/system-prompt'
import { createClient } from '@/lib/supabase/server'

/**
 * Onboarding chat — POST `/api/onboarding/chat` (ENT-44).
 *
 * Wire shape (set by `prepareSendMessagesRequest` on the client transport):
 *
 *     { message: UIMessage }   // only the newest user turn
 *
 * The server is the source of truth for the active session — never trusts a
 * client-supplied session id — so resume works without the URL carrying it.
 *
 * Persistence runs inside `toUIMessageStreamResponse({ onFinish })` and uses
 * `consumeStream()` so the assistant turn still lands in the DB even when
 * the client disconnects mid-stream.
 */

// Server-side stable IDs are required for persistence to round-trip.
const generateMessageId = createIdGenerator({ prefix: 'msg', size: 16 })

// Configurable via env so we can swap models without code churn.
const MODEL_ID = process.env.ONBOARDING_CHAT_MODEL ?? 'gpt-5.4-mini'

export async function POST(req: Request) {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    return NextResponse.json({ error: 'unauthorised' }, { status: 401 })
  }

  let body: { message: UIMessage }
  try {
    body = (await req.json()) as { message: UIMessage }
  } catch {
    return NextResponse.json({ error: 'invalid_json' }, { status: 400 })
  }
  if (!body?.message?.role || !Array.isArray(body.message.parts)) {
    return NextResponse.json({ error: 'invalid_message' }, { status: 400 })
  }

  const sessionId = await getOrCreateActiveSession(supabase, user.id)

  const transcriptRows = await loadTranscript(supabase, sessionId)
  const previousMessages: UIMessage[] = transcriptRows.map(uiMessageFromRow)
  const allMessages: UIMessage[] = [...previousMessages, body.message]

  // Use the Responses API (default for `@ai-sdk/openai`'s `openai()` callable).
  // OpenAI project keys can be scoped to /responses only, so this is the
  // safer default for environments where the user might issue a narrow key.
  const result = streamText({
    model: openai(MODEL_ID),
    system: ONBOARDING_SYSTEM_PROMPT,
    messages: await convertToModelMessages(allMessages),
  })

  return result.toUIMessageStreamResponse({
    originalMessages: allMessages,
    generateMessageId,
    onFinish: async ({ messages }) => {
      // `messages` is the full updated transcript (previous + new user + new
      // assistant). Append only the rows that aren't in the DB yet, and let
      // `messagesToPersist` drop empty assistant turns from failed model
      // calls (ENT-87) so the next prompt's context stays clean.
      const newMessages = messages.slice(previousMessages.length)
      const rows = messagesToPersist(newMessages)
      if (rows.length === 0) return
      await appendMessages(supabase, {
        sessionId,
        userId: user.id,
        messages: rows,
      })
    },
  })
}
