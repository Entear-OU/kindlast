import { NextRequest } from 'next/server'
import { cookies } from 'next/headers'
import { createUIMessageStream, createUIMessageStreamResponse } from 'ai'
import { getApiConfig, buildApiUrl } from '@/lib/api/config'

// Types matching backend SSE events
interface RAGChunk {
  type: 'content' | 'citation' | 'metadata' | 'error' | 'done'
  text?: string
  citation?: {
    source: string
    title: string
    url: string
    excerpt: string
    relevance: number
  }
  metadata?: {
    confidenceOk: boolean
    maxRelevance: number
    citationCount: number
  }
  error?: string
}

// Parse SSE event from RAG service
function parseSSEEvent(event: string): RAGChunk | null {
  const lines = event.split('\n')
  let data: string | null = null

  for (const line of lines) {
    if (line.startsWith('data:')) {
      data = line.slice(5).trim()
    }
  }

  if (!data) return null

  try {
    return JSON.parse(data) as RAGChunk
  } catch {
    return null
  }
}

export async function POST(request: NextRequest) {
  try {
    const config = getApiConfig()
    const cookieStore = await cookies()
    const accessToken = cookieStore.get(config.accessTokenCookie)?.value

    if (!accessToken) {
      return new Response(
        JSON.stringify({ error: 'Unauthorized', message: 'Missing authorization', code: 'UNAUTHORIZED' }),
        { status: 401, headers: { 'Content-Type': 'application/json' } }
      )
    }

    const body = await request.json()
    const gatewayUrl = buildApiUrl('/api/v1/query', config)

    // Request streaming from backend
    const response = await fetch(gatewayUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${accessToken}`,
        'Accept': 'text/event-stream',
      },
      body: JSON.stringify({ ...body, stream: true }),
    })

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({ error: 'Request failed' }))
      return new Response(
        JSON.stringify(errorData),
        { status: response.status, headers: { 'Content-Type': 'application/json' } }
      )
    }

    if (!response.body) {
      return new Response(
        JSON.stringify({ error: 'No response body' }),
        { status: 500, headers: { 'Content-Type': 'application/json' } }
      )
    }

    // Create UI message stream for AI SDK compatibility
    const stream = createUIMessageStream({
      execute: async ({ writer }) => {
        const reader = response.body!.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        const citations: RAGChunk['citation'][] = []
        let metadata: RAGChunk['metadata'] | null = null
        const textId = `text-${Date.now()}`
        let textStarted = false

        try {
          while (true) {
            const { done, value } = await reader.read()
            if (done) break

            buffer += decoder.decode(value, { stream: true })
            const events = buffer.split('\n\n')
            buffer = events.pop() || ''

            for (const event of events) {
              if (!event.trim()) continue

              const chunk = parseSSEEvent(event)
              if (!chunk) continue

              switch (chunk.type) {
                case 'content':
                  if (chunk.text) {
                    // Start text block on first content
                    if (!textStarted) {
                      writer.write({ type: 'text-start', id: textId })
                      textStarted = true
                    }
                    // Stream text delta
                    writer.write({ type: 'text-delta', id: textId, delta: chunk.text })
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
                    // Send error as data part
                    writer.write({
                      type: 'data-error' as const,
                      id: 'error',
                      data: { message: chunk.error },
                    })
                  }
                  break

                case 'done':
                  // End text block
                  if (textStarted) {
                    writer.write({ type: 'text-end', id: textId })
                  }
                  // Send citations as data part
                  if (citations.length > 0) {
                    writer.write({
                      type: 'data-citations' as const,
                      id: 'citations',
                      data: { citations },
                    })
                  }
                  // Send metadata as data part
                  if (metadata) {
                    writer.write({
                      type: 'data-metadata' as const,
                      id: 'metadata',
                      data: metadata,
                    })
                  }
                  break
              }
            }
          }

          // Process remaining buffer and ensure text is closed
          if (buffer.trim()) {
            const chunk = parseSSEEvent(buffer)
            if (chunk?.type === 'content' && chunk.text) {
              if (!textStarted) {
                writer.write({ type: 'text-start', id: textId })
                textStarted = true
              }
              writer.write({ type: 'text-delta', id: textId, delta: chunk.text })
            }
          }

          // Ensure text block is closed
          if (textStarted) {
            writer.write({ type: 'text-end', id: textId })
          }
        } finally {
          reader.releaseLock()
        }
      },
    })

    return createUIMessageStreamResponse({ stream })
  } catch (error) {
    console.error('Query proxy error:', error)
    return new Response(
      JSON.stringify({ error: 'Internal server error', code: 'INTERNAL_ERROR' }),
      { status: 500, headers: { 'Content-Type': 'application/json' } }
    )
  }
}
