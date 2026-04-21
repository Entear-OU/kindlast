'use client'

import { useChat } from '@ai-sdk/react'
import { DefaultChatTransport } from 'ai'
import { useState, useCallback, useMemo, useRef } from 'react'

export type QueryTopic = 'gdpr' | 'ai_act' | 'both'

export interface Citation {
  source: string
  title: string
  url: string
  excerpt: string
  relevance: number
}

export interface QueryMetadata {
  confidenceOk: boolean
  maxRelevance: number
  citationCount: number
}

export interface UseComplianceQueryReturn {
  /** The streamed response text */
  answer: string
  /** Citations from RAG */
  citations: Citation[]
  /** Metadata about the query */
  metadata: QueryMetadata | null
  /** Whether a query is in progress */
  isLoading: boolean
  /** Error message if any */
  error: string | null
  /** Execute a query */
  submitQuery: (query: string, topic: QueryTopic) => void
  /** Reset the state */
  reset: () => void
}

/**
 * Hook for querying the compliance RAG service using Vercel AI SDK
 */
export function useComplianceQuery(): UseComplianceQueryReturn {
  const [citations, setCitations] = useState<Citation[]>([])
  const [metadata, setMetadata] = useState<QueryMetadata | null>(null)
  const currentTopicRef = useRef<QueryTopic>('gdpr')

  // Create transport with custom API endpoint and dynamic body
  const transportRef = useRef(
    new DefaultChatTransport({
      api: '/api/query',
      body: () => ({
        topic: currentTopicRef.current,
      }),
    })
  )

  const {
    messages,
    status,
    error,
    sendMessage,
    setMessages,
  } = useChat({
    transport: transportRef.current,
    // Handle custom data parts (citations, metadata)
    onData: (dataPart) => {
      const part = dataPart as { type: string; data?: unknown }
      if (part.type === 'data-citations' && part.data) {
        const citationsData = part.data as { citations: Citation[] }
        setCitations(citationsData.citations || [])
      }
      if (part.type === 'data-metadata' && part.data) {
        const metadataData = part.data as QueryMetadata
        setMetadata(metadataData)
      }
    },
    onError: (err) => {
      console.error('Query error:', err)
    },
  })

  // Get the latest assistant message content from parts
  const answer = useMemo(() => {
    const lastAssistantMessage = messages.findLast((m) => m.role === 'assistant')
    if (!lastAssistantMessage?.parts) return ''

    // Concatenate all text parts
    return lastAssistantMessage.parts
      .filter((part): part is { type: 'text'; text: string } => part.type === 'text')
      .map((part) => part.text)
      .join('')
  }, [messages])

  // Derive loading state from status
  const isLoading = status === 'submitted' || status === 'streaming'

  const submitQuery = useCallback(
    (query: string, topic: QueryTopic) => {
      // Reset state for new query
      setCitations([])
      setMetadata(null)
      currentTopicRef.current = topic
      setMessages([])

      // Send the query - topic is injected via transport body
      sendMessage({ text: query })
    },
    [sendMessage, setMessages]
  )

  const reset = useCallback(() => {
    setMessages([])
    setCitations([])
    setMetadata(null)
  }, [setMessages])

  return {
    answer,
    citations,
    metadata,
    isLoading,
    error: error?.message || null,
    submitQuery,
    reset,
  }
}
