'use client'

import { useState, useCallback, useRef } from 'react'
import {
  parseSSEStream,
  StreamError,
  type Citation,
  type StreamMetadata,
  type StreamChunk,
} from '@/lib/api/streaming'

/**
 * Topic type matching backend prompts.Topic
 */
export type QueryTopic = 'gdpr' | 'ai_act' | 'both'

/**
 * Options for useStreamingQuery hook
 */
export interface UseStreamingQueryOptions {
  /**
   * Base URL for the RAG API
   * @default process.env.NEXT_PUBLIC_RAG_API_URL || ''
   */
  baseUrl?: string

  /**
   * Regulatory topic to query
   * @default 'both'
   */
  topic?: QueryTopic

  /**
   * Number of top results to retrieve
   */
  topK?: number
}

/**
 * Return type for useStreamingQuery hook
 */
export interface UseStreamingQueryReturn {
  /**
   * Accumulated response text from the stream
   */
  data: string

  /**
   * Citations extracted from the stream
   */
  citations: Citation[]

  /**
   * Whether a query is currently streaming
   */
  isStreaming: boolean

  /**
   * Error message if an error occurred
   */
  error: string | null

  /**
   * Metadata from the RAG pipeline
   */
  metadata: StreamMetadata | null

  /**
   * Execute a streaming query
   */
  executeQuery: (query: string) => void

  /**
   * Reset the hook state
   */
  reset: () => void

  /**
   * Abort the current streaming query
   */
  abort: () => void
}

/**
 * React hook for consuming streaming RAG responses
 *
 * @param options - Configuration options for the hook
 * @returns Object with data, citations, streaming state, and control functions
 *
 * @example
 * ```tsx
 * function QueryComponent() {
 *   const { data, citations, isStreaming, error, executeQuery } = useStreamingQuery({
 *     topic: 'gdpr',
 *   })
 *
 *   return (
 *     <div>
 *       <button onClick={() => executeQuery('What is Article 5?')}>
 *         Ask Question
 *       </button>
 *       {isStreaming && <p>Loading...</p>}
 *       {error && <p>Error: {error}</p>}
 *       <div>{data}</div>
 *       <ul>
 *         {citations.map((c, i) => (
 *           <li key={i}>{c.title} - {c.source}</li>
 *         ))}
 *       </ul>
 *     </div>
 *   )
 * }
 * ```
 */
export function useStreamingQuery(
  options: UseStreamingQueryOptions = {}
): UseStreamingQueryReturn {
  const {
    baseUrl = process.env.NEXT_PUBLIC_RAG_API_URL || '',
    topic = 'both',
    topK,
  } = options

  // State
  const [data, setData] = useState('')
  const [citations, setCitations] = useState<Citation[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [metadata, setMetadata] = useState<StreamMetadata | null>(null)

  // Abort controller ref for cancellation
  const abortControllerRef = useRef<AbortController | null>(null)

  /**
   * Reset all state to initial values
   */
  const reset = useCallback(() => {
    setData('')
    setCitations([])
    setIsStreaming(false)
    setError(null)
    setMetadata(null)
  }, [])

  /**
   * Abort the current streaming query
   */
  const abort = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
      abortControllerRef.current = null
    }
    setIsStreaming(false)
  }, [])

  /**
   * Process a stream chunk and update state
   */
  const processChunk = useCallback((chunk: StreamChunk) => {
    switch (chunk.type) {
      case 'content':
        if (chunk.text) {
          setData((prev) => prev + chunk.text)
        }
        break

      case 'citation':
        if (chunk.citation) {
          setCitations((prev) => [...prev, chunk.citation!])
        }
        break

      case 'metadata':
        if (chunk.metadata) {
          setMetadata(chunk.metadata)
        }
        break

      case 'error':
        if (chunk.error) {
          setError(chunk.error)
        }
        break

      case 'done':
        // Stream completed - no additional action needed
        break
    }
  }, [])

  /**
   * Execute a streaming query
   */
  const executeQuery = useCallback(
    (query: string) => {
      // Abort any existing query
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }

      // Reset state for new query
      setData('')
      setCitations([])
      setError(null)
      setMetadata(null)
      setIsStreaming(true)

      // Create new abort controller
      const abortController = new AbortController()
      abortControllerRef.current = abortController

      // Build request body
      const requestBody: Record<string, unknown> = {
        query,
        topic,
        stream: true,
      }

      if (topK !== undefined) {
        requestBody.topK = topK
      }

      // Execute fetch and process stream
      const runQuery = async () => {
        try {
          const response = await fetch(`${baseUrl}/api/v1/query`, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
            },
            body: JSON.stringify(requestBody),
            signal: abortController.signal,
          })

          // Parse and process the SSE stream
          for await (const chunk of parseSSEStream(response)) {
            // Check if aborted
            if (abortController.signal.aborted) {
              break
            }
            processChunk(chunk)
          }
        } catch (err) {
          // Ignore abort errors
          if (err instanceof Error && err.name === 'AbortError') {
            return
          }

          // Handle stream errors
          if (err instanceof StreamError) {
            setError(err.message)
          } else if (err instanceof Error) {
            setError(err.message)
          } else {
            setError('An unknown error occurred')
          }
        } finally {
          setIsStreaming(false)
          // Clear abort controller if this is the current one
          if (abortControllerRef.current === abortController) {
            abortControllerRef.current = null
          }
        }
      }

      runQuery()
    },
    [baseUrl, topic, topK, processChunk]
  )

  return {
    data,
    citations,
    isStreaming,
    error,
    metadata,
    executeQuery,
    reset,
    abort,
  }
}

// Re-export types from streaming module for convenience
export type { Citation, StreamMetadata }
