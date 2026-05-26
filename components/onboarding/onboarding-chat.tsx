'use client'

import { useChat } from '@ai-sdk/react'
import { DefaultChatTransport, type UIMessage } from 'ai'
import { AlertCircleIcon, ArrowUpIcon, RotateCcwIcon } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'

import {
  Message,
  MessageContent,
} from '@/components/ai-elements/message'
import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
} from '@/components/ai-elements/prompt-input'
import { Button } from '@/components/ui/button'

import { PostureSummaryCard } from '@/components/onboarding/posture-summary-card'
import type { PostureSummary } from '@/lib/onboarding/posture-summary'

function DraftingIndicator() {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex gap-1">
        <span className="size-2 animate-bounce rounded-full bg-foreground/40 [animation-delay:0ms]" />
        <span className="size-2 animate-bounce rounded-full bg-foreground/40 [animation-delay:150ms]" />
        <span className="size-2 animate-bounce rounded-full bg-foreground/40 [animation-delay:300ms]" />
      </div>
      <span className="text-sm text-muted-foreground">
        Drafting your compliance posture…
      </span>
    </div>
  )
}

export function OnboardingChat({
  sessionId,
  initialMessages,
  initialSummary,
}: {
  sessionId: string
  initialMessages: UIMessage[]
  initialSummary: PostureSummary | null
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

  // Walk the message parts for a `complete_onboarding` tool result. We pick
  // the latest `output-available` part with `status === 'completed'` so a
  // streamed completion mid-session overrides the server-loaded summary.
  // The server-provided `initialSummary` is the fallback for page reloads.
  const liveSummary = useMemo<PostureSummary | null>(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      const message = messages[i]
      for (const part of message.parts) {
        const partType = part.type as string
        const isCompleteTool =
          partType === 'tool-complete_onboarding' ||
          (partType === 'dynamic-tool' &&
            (part as { toolName?: string }).toolName === 'complete_onboarding')
        if (!isCompleteTool) continue
        const output = (part as { output?: unknown }).output as
          | { status?: string; summary?: PostureSummary }
          | undefined
        if (output && output.status === 'completed' && output.summary) {
          return output.summary
        }
      }
    }
    return null
  }, [messages])

  const summary = liveSummary ?? initialSummary
  const isCompleted = summary !== null

  // The "drafting" indicator only fires when the tool is in-flight — once
  // `summary` is set, the card takes its place and we never want both
  // visible at the same time.
  const isDrafting = useMemo(() => {
    if (isCompleted) return false
    for (const message of messages) {
      for (const part of message.parts) {
        const partType = part.type as string
        if (
          partType === 'tool-complete_onboarding' ||
          (partType === 'dynamic-tool' &&
            (part as { toolName?: string }).toolName === 'complete_onboarding')
        ) {
          return true
        }
      }
    }
    return false
  }, [messages, isCompleted])

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
  }, [messages, status, summary])

  const showError = status === 'error'
  // After completion the session is `completed` server-side; if we left
  // the input enabled, the next message would silently create a fresh
  // `in_progress` session via `getOrCreateActiveSession` — and the founder
  // would find themselves being re-interviewed. Lock it instead.
  const inputDisabled = isCompleted || isDrafting || status !== 'ready'

  return (
    <main className="mx-auto flex min-h-0 w-full max-w-2xl flex-1 flex-col px-4 pb-4 pt-6">
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div className="flex flex-col gap-6 py-4">
          {messages.map((message) => {
            const text = message.parts
              .filter((p) => p.type === 'text')
              .map((p) => (p as { type: 'text'; text: string }).text)
              .join('')
            if (!text) return null
            return (
              <Message key={message.id} from={message.role}>
                <MessageContent>
                  <p className="whitespace-pre-wrap">{text}</p>
                </MessageContent>
              </Message>
            )
          })}
          {isDrafting && status !== 'ready' && <DraftingIndicator />}
          {summary && <PostureSummaryCard summary={summary} />}
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

      {!isCompleted && (
        <div className="shrink-0 pb-2">
          <PromptInput
            className={`rounded-full border bg-background shadow-[0_1px_8px_rgba(0,0,0,0.06)] [&_[data-slot=input-group]]:h-auto [&_[data-slot=input-group]]:items-center [&_[data-slot=input-group]]:rounded-full [&_[data-slot=input-group]]:border-none [&_[data-slot=input-group-addon]]:static [&_[data-slot=input-group-addon]]:w-auto [&_[data-slot=input-group-addon]]:p-0 [&_[data-slot=input-group-addon]]:pr-2 ${isDrafting ? 'pointer-events-none opacity-50' : ''}`}
            onSubmit={({ text }) => {
              if (isDrafting) return
              const trimmed = text.trim()
              if (!trimmed) return
              pendingInputRef.current = trimmed
              sendMessage({ text: trimmed })
            }}
          >
            <PromptInputBody>
              <PromptInputTextarea
                className="min-h-0 px-5 py-2.5 text-sm"
                placeholder={isDrafting ? 'Drafting in progress…' : 'Type your answer…'}
                value={input}
                disabled={isDrafting}
                style={{ fieldSizing: 'normal' } as unknown as React.CSSProperties}
                onChange={(event) => setInput(event.currentTarget.value)}
              />
              <PromptInputFooter>
                <div />
                <PromptInputSubmit
                  status={status}
                  disabled={!input.trim() || inputDisabled}
                  className="size-8 rounded-full"
                >
                  <ArrowUpIcon className="size-4" />
                </PromptInputSubmit>
              </PromptInputFooter>
            </PromptInputBody>
          </PromptInput>
        </div>
      )}
    </main>
  )
}
