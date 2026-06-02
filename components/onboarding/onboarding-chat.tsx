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

function DraftingIndicator({ label = 'Drafting your compliance posture…' }: { label?: string }) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex gap-1">
        <span className="size-2 animate-bounce rounded-full bg-foreground/40 [animation-delay:0ms]" />
        <span className="size-2 animate-bounce rounded-full bg-foreground/40 [animation-delay:150ms]" />
        <span className="size-2 animate-bounce rounded-full bg-foreground/40 [animation-delay:300ms]" />
      </div>
      <span className="text-sm text-muted-foreground">{label}</span>
    </div>
  )
}

/** The concatenated text of a UI message — its `text` parts joined. */
function messageText(message: UIMessage): string {
  return message.parts
    .filter((p) => p.type === 'text')
    .map((p) => (p as { type: 'text'; text: string }).text)
    .join('')
}

// ENT-154: a hidden user turn the client sends to ask the server for the
// opening question (`{ begin: true }` on the wire). It never renders and is
// never persisted — it only exists to drive the first assistant stream.
export const OPENING_TRIGGER = '__begin_onboarding__'

export function OnboardingChat({
  sessionId,
  initialMessages,
  initialSummary,
  startOpening = false,
}: {
  sessionId: string
  initialMessages: UIMessage[]
  initialSummary: PostureSummary | null
  /** True on a fresh session — the client streams the opening question in. */
  startOpening?: boolean
}) {
  const [input, setInput] = useState('')
  const pendingInputRef = useRef<string | null>(null)
  const openingStartedRef = useRef(false)
  const scrollRef = useRef<HTMLDivElement>(null)

  const { messages, sendMessage, regenerate, status, error } = useChat({
    id: sessionId,
    messages: initialMessages,
    transport: new DefaultChatTransport({
      api: '/api/onboarding/chat',
      prepareSendMessagesRequest({ messages }) {
        const last = messages[messages.length - 1]
        // The opening trigger carries no real user content — ask the server to
        // open the interview instead of sending it as a turn.
        if (last?.role === 'user' && messageText(last) === OPENING_TRIGGER) {
          return { body: { begin: true } }
        }
        return { body: { message: last } }
      },
    }),
  })

  // ENT-154: on a fresh session the page renders instantly with an empty
  // transcript and we stream the opening question in here — once — instead of
  // blocking the server render on generation. Guarded by a ref so it fires
  // exactly once even under re-render or strict-mode double-invocation.
  useEffect(() => {
    if (startOpening && !openingStartedRef.current && messages.length === 0 && status === 'ready') {
      openingStartedRef.current = true
      sendMessage({ text: OPENING_TRIGGER })
    }
  }, [startOpening, messages.length, status, sendMessage])

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
      // Clear the input once the assistant has answered the just-sent turn.
      // Guarded by the ref (set back to null immediately), so this runs at most
      // once per send and can't cascade — intentional sync of async chat state.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setInput('')
      pendingInputRef.current = null
    }
  }, [status, lastMessage])

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages, status, summary])

  // ENT-154: while the opening question is being streamed on a fresh session,
  // the only message is the hidden trigger — show a typing indicator until the
  // assistant's first token lands (becomes a visible message).
  const hasVisibleMessage = messages.some(
    (m) => !(m.role === 'user' && messageText(m) === OPENING_TRIGGER) && messageText(m) !== '',
  )
  const openingPending = startOpening && !hasVisibleMessage && status !== 'ready'

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
            const text = messageText(message)
            // Never surface the hidden opening trigger (ENT-154).
            if (message.role === 'user' && text === OPENING_TRIGGER) return null
            if (!text) return null
            return (
              <Message key={message.id} from={message.role}>
                <MessageContent>
                  <p className="whitespace-pre-wrap">{text}</p>
                </MessageContent>
              </Message>
            )
          })}
          {/* ENT-154: the opening question streams in on a fresh session — show
              a typing indicator until its first token lands. */}
          {openingPending && <DraftingIndicator label="Starting your interview…" />}
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
