'use client'

import { useChat } from '@ai-sdk/react'
import { DefaultChatTransport, type UIMessage } from 'ai'
import { AlertCircleIcon, ArrowUpIcon, RotateCcwIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

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

export function OnboardingChat({
  sessionId,
  initialMessages,
}: {
  sessionId: string
  initialMessages: UIMessage[]
}) {
  const [input, setInput] = useState('')
  const pendingInputRef = useRef<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  const { messages, sendMessage, regenerate, status, error } = useChat({
    id: sessionId,
    messages: initialMessages,
    transport: new DefaultChatTransport({
      api: '/api/onboarding/chat',
      prepareSendMessagesRequest({ messages }) {
        return { body: { message: messages[messages.length - 1] } }
      },
    }),
  })

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

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages, status])

  const showError = status === 'error'

  return (
    <main className="mx-auto flex min-h-0 w-full max-w-2xl flex-1 flex-col px-4 pb-4 pt-6">
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="flex flex-col gap-6 py-4">
          {messages.map((message) => (
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
          ))}
        </div>
      </div>

      {showError && (
        <div
          role="alert"
          className="mb-3 flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm"
        >
          <AlertCircleIcon className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden />
          <div className="flex-1">
            <p className="font-medium text-foreground">Something went wrong.</p>
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
          {error?.message && (
            <span className="sr-only">Error: {error.message}</span>
          )}
        </div>
      )}

      <div className="shrink-0 pb-2">
        <PromptInput
          className="rounded-full border bg-background shadow-[0_1px_8px_rgba(0,0,0,0.06)] [&_[data-slot=input-group]]:h-auto [&_[data-slot=input-group]]:items-center [&_[data-slot=input-group]]:rounded-full [&_[data-slot=input-group]]:border-none [&_[data-slot=input-group-addon]]:static [&_[data-slot=input-group-addon]]:w-auto [&_[data-slot=input-group-addon]]:p-0 [&_[data-slot=input-group-addon]]:pr-2"
          onSubmit={({ text }) => {
            const trimmed = text.trim()
            if (!trimmed) return
            pendingInputRef.current = trimmed
            sendMessage({ text: trimmed })
          }}
        >
          <PromptInputBody>
            <PromptInputTextarea
              className="min-h-0 px-5 py-2.5 text-sm"
              placeholder="Type your answer…"
              value={input}
              style={{ fieldSizing: 'normal' } as React.CSSProperties}
              onChange={(event) => setInput(event.currentTarget.value)}
            />
            <PromptInputFooter>
              <div />
              <PromptInputSubmit
                status={status}
                disabled={!input.trim()}
                className="size-8 rounded-full"
              >
                <ArrowUpIcon className="size-4" />
              </PromptInputSubmit>
            </PromptInputFooter>
          </PromptInputBody>
        </PromptInput>
      </div>
    </main>
  )
}
