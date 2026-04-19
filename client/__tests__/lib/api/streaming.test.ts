import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  parseSSEStream,
  StreamChunk,
  ChunkType,
  Citation,
  StreamError,
} from '@/lib/api/streaming'

// Helper to create a mock ReadableStream from SSE events
function createMockSSEStream(events: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  let index = 0

  return new ReadableStream({
    pull(controller) {
      if (index < events.length) {
        controller.enqueue(encoder.encode(events[index]))
        index++
      } else {
        controller.close()
      }
    },
  })
}

// Helper to create SSE event string
function sseEvent(eventType: string, data: object | string): string {
  const dataStr = typeof data === 'string' ? data : JSON.stringify(data)
  return `event: ${eventType}\ndata: ${dataStr}\n\n`
}

// Helper to collect all chunks from async generator
async function collectChunks(
  stream: AsyncGenerator<StreamChunk>
): Promise<StreamChunk[]> {
  const chunks: StreamChunk[] = []
  for await (const chunk of stream) {
    chunks.push(chunk)
  }
  return chunks
}

describe('parseSSEStream', () => {
  describe('content chunks', () => {
    it('parses a single content chunk', async () => {
      const events = [sseEvent('content', { type: 'content', text: 'Hello' })]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(1)
      expect(chunks[0].type).toBe('content')
      expect(chunks[0].text).toBe('Hello')
    })

    it('parses multiple content chunks', async () => {
      const events = [
        sseEvent('content', { type: 'content', text: 'Hello' }),
        sseEvent('content', { type: 'content', text: ' world' }),
        sseEvent('content', { type: 'content', text: '!' }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(3)
      expect(chunks.map((c) => c.text).join('')).toBe('Hello world!')
    })
  })

  describe('citation chunks', () => {
    it('parses citation chunks', async () => {
      const citation: Citation = {
        source: 'GDPR',
        title: 'Article 5',
        url: 'https://eur-lex.europa.eu/gdpr/art5',
        excerpt: 'Personal data shall be processed lawfully...',
        relevance: 0.95,
      }
      const events = [
        sseEvent('citation', { type: 'citation', citation }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(1)
      expect(chunks[0].type).toBe('citation')
      expect(chunks[0].citation).toEqual(citation)
    })

    it('parses multiple citations before content', async () => {
      const citation1: Citation = {
        source: 'GDPR',
        title: 'Article 5',
        url: 'https://example.com/1',
        excerpt: 'Excerpt 1',
        relevance: 0.95,
      }
      const citation2: Citation = {
        source: 'EDPB Guidelines',
        title: 'Guideline 1',
        url: 'https://example.com/2',
        excerpt: 'Excerpt 2',
        relevance: 0.88,
      }
      const events = [
        sseEvent('citation', { type: 'citation', citation: citation1 }),
        sseEvent('citation', { type: 'citation', citation: citation2 }),
        sseEvent('content', { type: 'content', text: 'Based on the sources...' }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(3)
      expect(chunks[0].type).toBe('citation')
      expect(chunks[1].type).toBe('citation')
      expect(chunks[2].type).toBe('content')
    })
  })

  describe('metadata chunks', () => {
    it('parses metadata chunks', async () => {
      const metadata = {
        confidenceOk: true,
        maxRelevance: 0.92,
        citationCount: 3,
      }
      const events = [
        sseEvent('metadata', { type: 'metadata', metadata }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(1)
      expect(chunks[0].type).toBe('metadata')
      expect(chunks[0].metadata).toEqual(metadata)
    })
  })

  describe('done chunks', () => {
    it('parses done chunk to signal stream completion', async () => {
      const events = [
        sseEvent('content', { type: 'content', text: 'Hello' }),
        sseEvent('done', { type: 'done' }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(2)
      expect(chunks[1].type).toBe('done')
    })
  })

  describe('error handling', () => {
    it('parses error chunks from the stream', async () => {
      const events = [
        sseEvent('error', { type: 'error', error: 'Something went wrong' }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(1)
      expect(chunks[0].type).toBe('error')
      expect(chunks[0].error).toBe('Something went wrong')
    })

    it('throws StreamError when response is not ok', async () => {
      const mockResponse = {
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        body: null,
      } as Response

      await expect(collectChunks(parseSSEStream(mockResponse))).rejects.toThrow(
        StreamError
      )
    })

    it('throws StreamError when response body is null', async () => {
      const mockResponse = {
        ok: true,
        body: null,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      await expect(collectChunks(parseSSEStream(mockResponse))).rejects.toThrow(
        StreamError
      )
    })

    it('throws StreamError for invalid content type', async () => {
      const mockResponse = {
        ok: true,
        body: createMockSSEStream([]),
        headers: new Headers({ 'content-type': 'application/json' }),
      } as Response

      await expect(collectChunks(parseSSEStream(mockResponse))).rejects.toThrow(
        StreamError
      )
    })

    it('handles malformed JSON gracefully', async () => {
      const events = ['event: content\ndata: {invalid json}\n\n']
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      // Should yield an error chunk for malformed JSON
      expect(chunks).toHaveLength(1)
      expect(chunks[0].type).toBe('error')
      expect(chunks[0].error).toContain('parse')
    })
  })

  describe('stream interruption', () => {
    it('handles stream that ends abruptly', async () => {
      // Simulate partial data
      const encoder = new TextEncoder()
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(encoder.encode('event: content\n'))
          controller.close()
        },
      })

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      // Should complete without throwing, but no valid chunks
      const chunks = await collectChunks(parseSSEStream(mockResponse))
      expect(chunks).toHaveLength(0)
    })

    it('handles chunks split across multiple reads', async () => {
      // Simulate a chunk that arrives in parts
      const encoder = new TextEncoder()
      const parts = [
        'event: content\n',
        'data: {"type":"content","text":"Hello"}\n',
        '\n',
      ]
      let index = 0

      const stream = new ReadableStream({
        pull(controller) {
          if (index < parts.length) {
            controller.enqueue(encoder.encode(parts[index]))
            index++
          } else {
            controller.close()
          }
        },
      })

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(1)
      expect(chunks[0].type).toBe('content')
      expect(chunks[0].text).toBe('Hello')
    })
  })

  describe('full stream scenario', () => {
    it('handles a complete RAG streaming response', async () => {
      const citation1: Citation = {
        source: 'GDPR',
        title: 'Article 5',
        url: 'https://example.com/1',
        excerpt: 'Data minimization...',
        relevance: 0.95,
      }
      const citation2: Citation = {
        source: 'EDPB Guidelines',
        title: 'Guideline 4/2019',
        url: 'https://example.com/2',
        excerpt: 'Data protection by design...',
        relevance: 0.89,
      }
      const metadata = {
        confidenceOk: true,
        maxRelevance: 0.95,
        citationCount: 2,
      }

      const events = [
        sseEvent('citation', { type: 'citation', citation: citation1 }),
        sseEvent('citation', { type: 'citation', citation: citation2 }),
        sseEvent('metadata', { type: 'metadata', metadata }),
        sseEvent('content', { type: 'content', text: 'According to ' }),
        sseEvent('content', { type: 'content', text: 'Article 5 of the GDPR [1], ' }),
        sseEvent('content', { type: 'content', text: 'personal data must be processed...' }),
        sseEvent('done', { type: 'done' }),
      ]
      const stream = createMockSSEStream(events)

      const mockResponse = {
        body: stream,
        ok: true,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response

      const chunks = await collectChunks(parseSSEStream(mockResponse))

      expect(chunks).toHaveLength(7)

      // Check citations came first
      const citations = chunks.filter((c) => c.type === 'citation')
      expect(citations).toHaveLength(2)

      // Check metadata
      const metadataChunks = chunks.filter((c) => c.type === 'metadata')
      expect(metadataChunks).toHaveLength(1)
      expect(metadataChunks[0].metadata?.confidenceOk).toBe(true)

      // Check content
      const contentChunks = chunks.filter((c) => c.type === 'content')
      expect(contentChunks).toHaveLength(3)
      expect(contentChunks.map((c) => c.text).join('')).toContain('Article 5')

      // Check done
      const doneChunks = chunks.filter((c) => c.type === 'done')
      expect(doneChunks).toHaveLength(1)
    })
  })
})
