/**
 * SSE Streaming utilities for RAG service responses
 *
 * This module provides utilities for parsing Server-Sent Events (SSE) streams
 * from the RAG service, matching the backend format defined in Go.
 */

/**
 * Chunk types matching backend ChunkType constants
 */
export type ChunkType = 'content' | 'citation' | 'metadata' | 'error' | 'done'

/**
 * Citation structure matching backend prompts.Citation
 */
export interface Citation {
  source: string
  title: string
  url: string
  excerpt: string
  relevance: number
}

/**
 * Metadata from the RAG pipeline
 */
export interface StreamMetadata {
  confidenceOk: boolean
  maxRelevance: number
  citationCount: number
}

/**
 * Stream chunk matching backend rag.StreamChunk
 */
export interface StreamChunk {
  type: ChunkType
  text?: string
  citation?: Citation
  error?: string
  metadata?: StreamMetadata
}

/**
 * Custom error class for stream-related errors
 */
export class StreamError extends Error {
  public readonly status?: number
  public readonly statusText?: string

  constructor(message: string, status?: number, statusText?: string) {
    super(message)
    this.name = 'StreamError'
    this.status = status
    this.statusText = statusText
  }
}

/**
 * Parse an SSE stream from a fetch Response into an async generator of StreamChunks
 *
 * @param response - The fetch Response object with SSE stream body
 * @yields StreamChunk objects parsed from SSE events
 * @throws StreamError if the response is not valid or stream parsing fails
 *
 * @example
 * ```typescript
 * const response = await fetch('/api/v1/query', {
 *   method: 'POST',
 *   body: JSON.stringify({ query: 'What is GDPR?', stream: true }),
 * })
 *
 * for await (const chunk of parseSSEStream(response)) {
 *   if (chunk.type === 'content') {
 *     console.log(chunk.text)
 *   } else if (chunk.type === 'citation') {
 *     console.log(chunk.citation)
 *   }
 * }
 * ```
 */
export async function* parseSSEStream(
  response: Response
): AsyncGenerator<StreamChunk> {
  // Validate response
  if (!response.ok) {
    throw new StreamError(
      `HTTP error: ${response.status} ${response.statusText}`,
      response.status,
      response.statusText
    )
  }

  if (!response.body) {
    throw new StreamError('Response body is null')
  }

  const contentType = response.headers.get('content-type')
  if (!contentType?.includes('text/event-stream')) {
    throw new StreamError(
      `Invalid content type: expected text/event-stream, got ${contentType}`
    )
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()

  // Buffer for accumulating partial SSE events
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()

      if (done) {
        break
      }

      // Decode the chunk and add to buffer
      buffer += decoder.decode(value, { stream: true })

      // Process complete SSE events from buffer
      // SSE events are separated by double newlines
      const events = buffer.split('\n\n')

      // Keep the last incomplete event in the buffer
      buffer = events.pop() || ''

      for (const event of events) {
        if (!event.trim()) continue

        const chunk = parseSSEEvent(event)
        if (chunk) {
          yield chunk
        }
      }
    }

    // Process any remaining data in buffer
    if (buffer.trim()) {
      const chunk = parseSSEEvent(buffer)
      if (chunk) {
        yield chunk
      }
    }
  } finally {
    reader.releaseLock()
  }
}

/**
 * Parse a single SSE event string into a StreamChunk
 *
 * SSE format from backend:
 * event: <type>
 * data: <json>
 *
 * @param event - Raw SSE event string
 * @returns Parsed StreamChunk or null if event is invalid
 */
function parseSSEEvent(event: string): StreamChunk | null {
  const lines = event.split('\n')

  let eventType: string | null = null
  let data: string | null = null

  for (const line of lines) {
    if (line.startsWith('event:')) {
      eventType = line.slice(6).trim()
    } else if (line.startsWith('data:')) {
      data = line.slice(5).trim()
    }
  }

  // Need both event type and data to parse
  if (!eventType || !data) {
    return null
  }

  try {
    const parsed = JSON.parse(data)
    return normalizeChunk(parsed)
  } catch {
    // Return error chunk for malformed JSON
    return {
      type: 'error',
      error: `Failed to parse SSE data: ${data}`,
    }
  }
}

/**
 * Normalize a parsed chunk to ensure consistent structure
 */
function normalizeChunk(parsed: unknown): StreamChunk {
  if (typeof parsed !== 'object' || parsed === null) {
    return {
      type: 'error',
      error: 'Invalid chunk: expected object',
    }
  }

  const chunk = parsed as Record<string, unknown>
  const type = chunk.type as ChunkType

  switch (type) {
    case 'content':
      return {
        type: 'content',
        text: chunk.text as string | undefined,
      }

    case 'citation':
      return {
        type: 'citation',
        citation: chunk.citation as Citation | undefined,
      }

    case 'metadata':
      return {
        type: 'metadata',
        metadata: chunk.metadata as StreamMetadata | undefined,
      }

    case 'error':
      return {
        type: 'error',
        error: chunk.error as string | undefined,
      }

    case 'done':
      return {
        type: 'done',
      }

    default:
      return {
        type: 'error',
        error: `Unknown chunk type: ${type}`,
      }
  }
}

/**
 * Helper function to accumulate stream chunks into final response
 *
 * @param stream - AsyncGenerator of StreamChunks
 * @returns Object containing accumulated text, citations, metadata, and any error
 */
export async function accumulateStream(
  stream: AsyncGenerator<StreamChunk>
): Promise<{
  text: string
  citations: Citation[]
  metadata: StreamMetadata | null
  error: string | null
}> {
  let text = ''
  const citations: Citation[] = []
  let metadata: StreamMetadata | null = null
  let error: string | null = null

  for await (const chunk of stream) {
    switch (chunk.type) {
      case 'content':
        if (chunk.text) {
          text += chunk.text
        }
        break
      case 'citation':
        if (chunk.citation) {
          citations.push(chunk.citation)
        }
        break
      case 'metadata':
        if (chunk.metadata) {
          metadata = chunk.metadata
        }
        break
      case 'error':
        if (chunk.error) {
          error = chunk.error
        }
        break
      case 'done':
        // Stream complete
        break
    }
  }

  return { text, citations, metadata, error }
}
