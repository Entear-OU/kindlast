import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useStreamingQuery } from '@/hooks/use-streaming-query'
import type { Citation } from '@/lib/api/streaming'

// Mock fetch
const mockFetch = vi.fn()
global.fetch = mockFetch

// Helper to create a mock SSE stream response
function createMockSSEResponse(events: string[]): Response {
  const encoder = new TextEncoder()
  let index = 0

  const stream = new ReadableStream({
    pull(controller) {
      if (index < events.length) {
        controller.enqueue(encoder.encode(events[index]))
        index++
      } else {
        controller.close()
      }
    },
  })

  return {
    ok: true,
    status: 200,
    statusText: 'OK',
    body: stream,
    headers: new Headers({ 'content-type': 'text/event-stream' }),
  } as Response
}

// Helper to create SSE event string
function sseEvent(eventType: string, data: object | string): string {
  const dataStr = typeof data === 'string' ? data : JSON.stringify(data)
  return `event: ${eventType}\ndata: ${dataStr}\n\n`
}

describe('useStreamingQuery', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('initial state', () => {
    it('returns initial state before query is executed', () => {
      const { result } = renderHook(() => useStreamingQuery())

      expect(result.current.data).toBe('')
      expect(result.current.citations).toEqual([])
      expect(result.current.isStreaming).toBe(false)
      expect(result.current.error).toBeNull()
      expect(result.current.metadata).toBeNull()
    })
  })

  describe('streaming query execution', () => {
    it('sets isStreaming to true when query starts', async () => {
      // Create a stream that doesn't complete immediately
      const events = [sseEvent('content', { type: 'content', text: 'Hello' })]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      expect(result.current.isStreaming).toBe(true)

      await waitFor(() => {
        expect(result.current.isStreaming).toBe(false)
      })
    })

    it('accumulates text content from content chunks', async () => {
      const events = [
        sseEvent('content', { type: 'content', text: 'Hello' }),
        sseEvent('content', { type: 'content', text: ' world' }),
        sseEvent('content', { type: 'content', text: '!' }),
        sseEvent('done', { type: 'done' }),
      ]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.data).toBe('Hello world!')
      })
    })

    it('extracts citations from citation chunks', async () => {
      const citation1: Citation = {
        source: 'GDPR',
        title: 'Article 5',
        url: 'https://example.com/1',
        excerpt: 'Data minimization...',
        relevance: 0.95,
      }
      const citation2: Citation = {
        source: 'EDPB',
        title: 'Guideline 1',
        url: 'https://example.com/2',
        excerpt: 'Personal data...',
        relevance: 0.88,
      }

      const events = [
        sseEvent('citation', { type: 'citation', citation: citation1 }),
        sseEvent('citation', { type: 'citation', citation: citation2 }),
        sseEvent('content', { type: 'content', text: 'Response text' }),
        sseEvent('done', { type: 'done' }),
      ]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.citations).toHaveLength(2)
        expect(result.current.citations[0]).toEqual(citation1)
        expect(result.current.citations[1]).toEqual(citation2)
      })
    })

    it('extracts metadata from metadata chunks', async () => {
      const metadata = {
        confidenceOk: true,
        maxRelevance: 0.92,
        citationCount: 2,
      }

      const events = [
        sseEvent('metadata', { type: 'metadata', metadata }),
        sseEvent('content', { type: 'content', text: 'Response' }),
        sseEvent('done', { type: 'done' }),
      ]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.metadata).toEqual(metadata)
      })
    })

    it('sets isStreaming to false when stream completes', async () => {
      const events = [
        sseEvent('content', { type: 'content', text: 'Response' }),
        sseEvent('done', { type: 'done' }),
      ]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.isStreaming).toBe(false)
      })
    })
  })

  describe('error handling', () => {
    it('sets error when fetch fails', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.error).toBe('Network error')
        expect(result.current.isStreaming).toBe(false)
      })
    })

    it('sets error when response is not ok', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        body: null,
      } as Response)

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.error).toContain('500')
        expect(result.current.isStreaming).toBe(false)
      })
    })

    it('sets error when stream contains error chunk', async () => {
      const events = [
        sseEvent('error', { type: 'error', error: 'Retrieval failed' }),
      ]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.error).toBe('Retrieval failed')
      })
    })
  })

  describe('query options', () => {
    it('sends correct request with default options', async () => {
      const events = [sseEvent('done', { type: 'done' })]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          expect.any(String),
          expect.objectContaining({
            method: 'POST',
            headers: expect.objectContaining({
              'Content-Type': 'application/json',
            }),
            body: expect.stringContaining('"stream":true'),
          })
        )
      })
    })

    it('includes topic in request when provided', async () => {
      const events = [sseEvent('done', { type: 'done' })]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() =>
        useStreamingQuery({ topic: 'gdpr' })
      )

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        const call = mockFetch.mock.calls[0]
        const body = JSON.parse(call[1].body)
        expect(body.topic).toBe('gdpr')
      })
    })

    it('includes topK in request when provided', async () => {
      const events = [sseEvent('done', { type: 'done' })]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() =>
        useStreamingQuery({ topK: 10 })
      )

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        const call = mockFetch.mock.calls[0]
        const body = JSON.parse(call[1].body)
        expect(body.topK).toBe(10)
      })
    })

    it('uses custom baseUrl when provided', async () => {
      const events = [sseEvent('done', { type: 'done' })]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() =>
        useStreamingQuery({ baseUrl: 'https://custom-api.example.com' })
      )

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith(
          'https://custom-api.example.com/api/v1/query',
          expect.any(Object)
        )
      })
    })
  })

  describe('reset functionality', () => {
    it('resets state when reset is called', async () => {
      const events = [
        sseEvent('content', { type: 'content', text: 'Response' }),
        sseEvent('done', { type: 'done' }),
      ]
      mockFetch.mockResolvedValueOnce(createMockSSEResponse(events))

      const { result } = renderHook(() => useStreamingQuery())

      // Execute query
      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      await waitFor(() => {
        expect(result.current.data).toBe('Response')
      })

      // Reset
      act(() => {
        result.current.reset()
      })

      expect(result.current.data).toBe('')
      expect(result.current.citations).toEqual([])
      expect(result.current.error).toBeNull()
      expect(result.current.metadata).toBeNull()
      expect(result.current.isStreaming).toBe(false)
    })
  })

  describe('multiple queries', () => {
    it('clears previous results when new query starts', async () => {
      const events1 = [
        sseEvent('content', { type: 'content', text: 'First response' }),
        sseEvent('done', { type: 'done' }),
      ]
      const events2 = [
        sseEvent('content', { type: 'content', text: 'Second response' }),
        sseEvent('done', { type: 'done' }),
      ]

      mockFetch
        .mockResolvedValueOnce(createMockSSEResponse(events1))
        .mockResolvedValueOnce(createMockSSEResponse(events2))

      const { result } = renderHook(() => useStreamingQuery())

      // First query
      act(() => {
        result.current.executeQuery('First query')
      })

      await waitFor(() => {
        expect(result.current.data).toBe('First response')
      })

      // Second query
      act(() => {
        result.current.executeQuery('Second query')
      })

      // Should clear immediately
      expect(result.current.data).toBe('')

      await waitFor(() => {
        expect(result.current.data).toBe('Second response')
      })
    })
  })

  describe('abort handling', () => {
    it('can abort an in-progress query', async () => {
      // Create a stream that waits indefinitely
      let controllerRef: ReadableStreamDefaultController<Uint8Array> | null = null
      const stream = new ReadableStream({
        start(controller) {
          controllerRef = controller
        },
      })

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        body: stream,
        headers: new Headers({ 'content-type': 'text/event-stream' }),
      } as Response)

      const { result } = renderHook(() => useStreamingQuery())

      act(() => {
        result.current.executeQuery('What is GDPR?')
      })

      expect(result.current.isStreaming).toBe(true)

      // Abort the query
      act(() => {
        result.current.abort()
      })

      await waitFor(() => {
        expect(result.current.isStreaming).toBe(false)
      })

      // Clean up the stream
      if (controllerRef) {
        controllerRef.close()
      }
    })
  })
})
