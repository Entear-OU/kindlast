import { openai } from '@ai-sdk/openai'
import {
  convertToModelMessages,
  createIdGenerator,
  stepCountIs,
  streamText,
  tool,
  type UIMessage,
} from 'ai'
import { NextResponse } from 'next/server'
import { z } from 'zod'

import { extractComplianceProfile, type TranscriptTurn } from '@/lib/onboarding/extraction'
import {
  appendMessages,
  getOrCreateActiveSession,
  loadTranscript,
  markSessionCompleted,
  messagesToPersist,
  persistComplianceProfile,
  textFromUIMessage,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'
import { computePostureSummary } from '@/lib/onboarding/posture-summary'
import { ONBOARDING_SYSTEM_PROMPT } from '@/lib/onboarding/system-prompt'
import { createClient } from '@/lib/supabase/server'

/**
 * Onboarding chat — POST `/api/onboarding/chat` (ENT-44 + ENT-45).
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
 *
 * Finalization (ENT-45): the agent has a `complete_onboarding` tool it calls
 * when it judges the interview complete. Its `inputSchema` makes the agent
 * tick off each of the six required topics; the server rechecks (a) that
 * every topic is ticked and (b) that the transcript actually has enough
 * user turns to back the claim — premature calls return `too_early` and
 * the model gets a second step to correct itself.
 *
 * Known dev-only quirk (ENT-89): the very first POST after a fresh
 * `pnpm dev` start can land an assistant message with its content
 * concatenated twice (cold Turbopack compile re-running the stream
 * consumer mid-request — `compile: ~1s` shows up in the route log).
 * Subsequent turns are clean. A `next build` does not reproduce: prod
 * pre-compiles every route handler so there's no mid-request compile
 * window for the stream to be doubled in. No defensive dedupe is added
 * here — the suspected fault is in the dev runtime, not our code, and
 * the canonical fix is "use `pnpm start` for any verification that
 * depends on cold-path timing".
 * Refs: https://linear.app/entear/issue/ENT-89.
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

  // The agent calls this when it judges the interview complete. It must
  // explicitly check off each of the six required topics in `topicsCovered`,
  // so a premature call surfaces as a missing topic rather than a corrupt
  // extraction. The extraction itself is a separate `generateObject` pass
  // with its own focused prompt — see `lib/onboarding/extraction.ts`.
  const completeOnboarding = tool({
    description:
      'Finalise the onboarding interview. You MUST have already asked about, and received a substantive answer for, every required topic. Pass true for a topic only when the founder has actually answered that specific question in their own words. If any topic is false, do not call this tool — ask the founder about the missing topic instead. Never call this tool on your first reply or before the founder has answered at least four turns.',
    inputSchema: z.object({
      topicsCovered: z.object({
        productOrService: z
          .boolean()
          .describe('Founder has explained what the company does in their own words.'),
        personalDataAndSubjects: z
          .boolean()
          .describe('Founder has named both what data is collected and from whom.'),
        euJurisdictions: z
          .boolean()
          .describe('Founder has named the EU/EEA countries their users / data subjects are in.'),
        aiTools: z
          .boolean()
          .describe('Founder has said which AI tools are in use, internal or product.'),
        dpoStatus: z
          .boolean()
          .describe('Founder has answered whether they have a Data Protection Officer.'),
        ropaStatus: z
          .boolean()
          .describe('Founder has answered whether they have a Record of Processing Activities.'),
      }),
    }),
    execute: async ({ topicsCovered }) => {
      const missing = Object.entries(topicsCovered)
        .filter(([, covered]) => !covered)
        .map(([topic]) => topic)
      if (missing.length > 0) {
        return {
          status: 'too_early' as const,
          missing,
          message:
            'You marked some topics as not covered. Ask the founder about the missing topics, then call this tool again once each is answered.',
        }
      }

      const transcript: TranscriptTurn[] = allMessages
        .map((message) => ({
          role: message.role as 'user' | 'assistant',
          content: textFromUIMessage(message),
        }))
        .filter((turn) => turn.content.trim() !== '' || turn.role === 'user')

      // Belt-and-braces: even if the model claims every topic is covered, a
      // transcript with fewer than four user turns can't plausibly carry
      // answers to six topics. Refuse instead of producing a placeholder
      // profile the founder will have to re-do later.
      const userTurns = transcript.filter((turn) => turn.role === 'user').length
      if (userTurns < 4) {
        return {
          status: 'too_early' as const,
          missing: [],
          message: `Only ${userTurns} user turn(s) so far. Keep interviewing — call this tool only after the founder has substantively answered all six topics.`,
        }
      }

      const profile = await extractComplianceProfile(transcript)
      await persistComplianceProfile(supabase, {
        sessionId,
        userId: user.id,
        profile,
      })
      await markSessionCompleted(supabase, sessionId)

      // ENT-46: deterministic posture projection — return it inline so the
      // client can render the summary card without a follow-up fetch. The
      // server page also computes the same summary on reload via
      // `loadComplianceProfile`, so the card survives a refresh.
      const summary = computePostureSummary(profile)
      return { status: 'completed' as const, summary }
    },
  })

  // Use the Responses API (default for `@ai-sdk/openai`'s `openai()` callable).
  // OpenAI project keys can be scoped to /responses only, so this is the
  // safer default for environments where the user might issue a narrow key.
  const result = streamText({
    model: openai(MODEL_ID),
    system: ONBOARDING_SYSTEM_PROMPT,
    messages: await convertToModelMessages(allMessages),
    tools: { complete_onboarding: completeOnboarding },
    // Allow a second model step so the agent can react to a `too_early`
    // tool result — either by asking the missing topic or by acknowledging
    // a `completed` finalisation — rather than stopping silently.
    stopWhen: stepCountIs(2),
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
