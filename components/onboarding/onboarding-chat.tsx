'use client'

import { useChat } from '@ai-sdk/react'
import { DefaultChatTransport, type UIMessage } from 'ai'
import { AlertCircleIcon, RotateCcwIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import {
  Message,
  MessageContent,
  MessageResponse,
} from '@/components/ai-elements/message'
import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
} from '@/components/ai-elements/prompt-input'
import { Button } from '@/components/ui/button'

/**
 * Conversational onboarding chat (ENT-44, client component).
 *
 * AI SDK v6 streaming with `useChat` + AI Elements UI primitives.
 *
 * Wire shape: only the newest user turn is sent to the server on each
 * submission (`prepareSendMessagesRequest`). The server is the source of
 * truth for the active session, so resume works without the URL carrying
 * any identifier — server-side `getOrCreateActiveSession` resolves it from
 * the authenticated user.
 *
 * Failure handling (ENT-88): a server-side `streamText` failure flips
 * `useChat().status` to `'error'`. We surface that as an inline banner
 * above the prompt with a Retry control (replays via `regenerate`) and
 * keep the user's typed text in the textarea so a hand-edit is also
 * possible. The clear-on-success is now an effect that fires once the
 * next assistant turn lands, instead of an unconditional clear at submit.
 */
export function OnboardingChat({
  sessionId,
  initialMessages,
}: {
  sessionId: string
  initialMessages: UIMessage[]
}) {
  const [input, setInput] = useState('')
  // Tracks the text we've just submitted; the clear-on-success effect uses
  // it to know whether the textarea still contains a pending submission.
  const pendingInputRef = useRef<string | null>(null)

  const { messages, sendMessage, regenerate, status, error } = useChat({
    id: sessionId,
    messages: initialMessages,
    transport: new DefaultChatTransport({
      api: '/api/onboarding/chat',
      // Send only the newest message — server loads the prior transcript.
      prepareSendMessagesRequest({ messages }) {
        return { body: { message: messages[messages.length - 1] } }
      },
    }),
  })

  // Clear the textarea only once the next assistant turn has streamed in.
  // If the call failed, `status` skips straight from `submitted`/`streaming`
  // to `'error'` without an assistant message, and we leave `input` intact.
  const lastMessage = messages[messages.length - 1]
  useEffect(() => {
    if (
      pendingInputRef.current !== null &&
      status === 'ready' &&
      lastMessage?.role === 'assistant'
    ) {
      setInput('')
      pendingInputRef.current = null
    }
  }, [status, lastMessage])

  const showError = status === 'error'

  return (
    <main className="mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col px-4 py-6">
      <header className="mb-4 flex flex-col gap-1">
        <p className="text-[12px] font-bold uppercase tracking-[0.18em] text-muted-foreground">
          Onboarding
        </p>
        <h1 className="text-balance font-black text-2xl tracking-tight">
          Let&apos;s build your compliance posture.
        </h1>
        <p className="text-muted-foreground text-sm">
          Six plain-language questions, about five to ten minutes. Your answers stay private and
          map to your initial GDPR and EU AI Act profile.
        </p>
      </header>

      <Conversation className="flex-1 rounded-lg border">
        <ConversationContent>
          {messages.length === 0 ? (
            <ConversationEmptyState
              title="Ready when you are."
              description="Type a quick hello to begin — the agent will take it from there."
            />
          ) : (
            messages.map((message) => (
              <Message key={message.id} from={message.role}>
                <MessageContent>
                  {message.role === 'assistant' ? (
                    <MessageResponse>
                      {message.parts
                        .filter((p) => p.type === 'text')
                        .map((p) => (p as { type: 'text'; text: string }).text)
                        .join('')}
                    </MessageResponse>
                  ) : (
                    message.parts
                      .filter((p) => p.type === 'text')
                      .map((p) => (p as { type: 'text'; text: string }).text)
                      .join('')
                  )}
                </MessageContent>
              </Message>
            ))
          )}
        </ConversationContent>
        <ConversationScrollButton />
      </Conversation>

      {showError && (
        <div
          role="alert"
          className="mt-4 flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm"
        >
          <AlertCircleIcon className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden />
          <div className="flex-1">
            <p className="font-medium text-foreground">
              Something went wrong — try again.
            </p>
            <p className="text-muted-foreground">
              Your last answer is still in the box below. We didn&apos;t lose it.
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => regenerate()}
            className="shrink-0"
          >
            <RotateCcwIcon className="mr-1.5 size-3.5" aria-hidden />
            Retry
          </Button>
          {/* Surface the underlying message only when meaningful — used for
              dev visibility; the founder-facing copy is above. */}
          {error?.message && (
            <span className="sr-only">Error: {error.message}</span>
          )}
        </div>
      )}

      <PromptInput
        className="mt-4"
        onSubmit={({ text }) => {
          const trimmed = text.trim()
          if (!trimmed) return
          pendingInputRef.current = trimmed
          sendMessage({ text: trimmed })
          // Don't clear `input` here — the effect above clears it only after
          // a successful round-trip so the user can resend on failure.
        }}
      >
        <PromptInputBody>
          <PromptInputTextarea
            placeholder="Type your answer…"
            value={input}
            onChange={(event) => setInput(event.currentTarget.value)}
          />
          <PromptInputFooter>
            <div />
            <PromptInputSubmit status={status} disabled={!input.trim()} />
          </PromptInputFooter>
        </PromptInputBody>
      </PromptInput>
    </main>
  )
}
