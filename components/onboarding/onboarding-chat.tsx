'use client'

import { useChat } from '@ai-sdk/react'
import { DefaultChatTransport, type UIMessage } from 'ai'
import { useState } from 'react'

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
 */
export function OnboardingChat({
  sessionId,
  initialMessages,
}: {
  sessionId: string
  initialMessages: UIMessage[]
}) {
  const [input, setInput] = useState('')

  const { messages, sendMessage, status } = useChat({
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

  return (
    <main className="mx-auto flex h-[100dvh] w-full max-w-3xl flex-col px-4 py-6">
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

      <PromptInput
        className="mt-4"
        onSubmit={({ text }) => {
          const trimmed = text.trim()
          if (!trimmed) return
          sendMessage({ text: trimmed })
          setInput('')
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
