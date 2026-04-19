import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type {
  QueryRequest,
  QueryResponse,
  StreamChunk,
  APIError,
} from '@/lib/api/types'

// Mock fetch globally
const mockFetch = vi.fn()
global.fetch = mockFetch

describe('Gateway Client', () => {
  const originalEnv = process.env

  beforeEach(() => {
    vi.clearAllMocks()
    vi.resetModules()
    process.env = { ...originalEnv, NEXT_PUBLIC_API_URL: 'https://api.kindlast.com' }
  })

  afterEach(() => {
    process.env = originalEnv
  })

  describe('getApiUrl', () => {
    it('returns the API URL from environment variable', async () => {
      const { getApiUrl } = await import('@/lib/api/gateway')
      expect(getApiUrl()).toBe('https://api.kindlast.com')
    })

    it('throws error when NEXT_PUBLIC_API_URL is not set', async () => {
      delete process.env.NEXT_PUBLIC_API_URL
      vi.resetModules()

      const { getApiUrl } = await import('@/lib/api/gateway')
      expect(() => getApiUrl()).toThrow('NEXT_PUBLIC_API_URL environment variable is not set')
    })

    it('removes trailing slash from URL', async () => {
      process.env.NEXT_PUBLIC_API_URL = 'https://api.kindlast.com/'
      vi.resetModules()

      const { getApiUrl } = await import('@/lib/api/gateway')
      expect(getApiUrl()).toBe('https://api.kindlast.com')
    })
  })

  describe('queryRAG (non-streaming)', () => {
    const mockQueryResponse: QueryResponse = {
      answer: 'GDPR requires data controllers to...',
      citations: [
        {
          source: 'GDPR',
          title: 'Article 5 - Principles',
          url: 'https://eur-lex.europa.eu/...',
          excerpt: 'Personal data shall be processed lawfully...',
          relevance: 0.95,
        },
      ],
      cacheHit: false,
      confidenceOk: true,
      maxRelevance: 0.95,
      processingTime: 1500,
    }

    it('sends POST request to /api/v1/query endpoint', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(mockQueryResponse),
      })

      const { queryRAG } = await import('@/lib/api/gateway')
      const request: QueryRequest = { query: 'What is GDPR Article 5?' }

      await queryRAG(request, 'test-token')

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.kindlast.com/api/v1/query',
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
            'Authorization': 'Bearer test-token',
          }),
        })
      )
    })

    it('sends query with topic filter', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(mockQueryResponse),
      })

      const { queryRAG } = await import('@/lib/api/gateway')
      const request: QueryRequest = {
        query: 'What is GDPR Article 5?',
        topic: 'gdpr',
        topK: 10,
      }

      await queryRAG(request, 'test-token')

      const callArgs = mockFetch.mock.calls[0]
      const body = JSON.parse(callArgs[1].body)
      expect(body.query).toBe('What is GDPR Article 5?')
      expect(body.topic).toBe('gdpr')
      expect(body.topK).toBe(10)
      expect(body.stream).toBe(false)
    })

    it('returns QueryResponse on success', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(mockQueryResponse),
      })

      const { queryRAG } = await import('@/lib/api/gateway')
      const result = await queryRAG({ query: 'Test query' }, 'test-token')

      expect(result).toEqual(mockQueryResponse)
      expect(result.citations).toHaveLength(1)
      expect(result.confidenceOk).toBe(true)
    })

    it('throws GatewayError on API error response', async () => {
      const apiError: APIError = {
        error: 'Bad Request',
        message: 'Query cannot be empty',
        code: 'VALIDATION_ERROR',
      }

      // Mock for the first assertion
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: () => Promise.resolve(apiError),
      })

      const { queryRAG, GatewayError } = await import('@/lib/api/gateway')

      await expect(queryRAG({ query: '' }, 'test-token')).rejects.toThrow(GatewayError)

      // Mock for the second assertion
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: () => Promise.resolve(apiError),
      })

      await expect(queryRAG({ query: '' }, 'test-token')).rejects.toMatchObject({
        message: 'Query cannot be empty',
        code: 'VALIDATION_ERROR',
        status: 400,
      })
    })

    it('throws GatewayError on network error', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'))

      const { queryRAG, GatewayError } = await import('@/lib/api/gateway')

      await expect(queryRAG({ query: 'Test' }, 'test-token')).rejects.toThrow(GatewayError)
    })

    it('handles 401 unauthorized error', async () => {
      const apiError: APIError = {
        error: 'Unauthorized',
        message: 'Invalid or expired token',
        code: 'INVALID_TOKEN',
      }

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () => Promise.resolve(apiError),
      })

      const { queryRAG, GatewayError } = await import('@/lib/api/gateway')

      await expect(queryRAG({ query: 'Test' }, 'invalid-token')).rejects.toMatchObject({
        code: 'INVALID_TOKEN',
        status: 401,
      })
    })

    it('handles 429 rate limit error', async () => {
      const apiError: APIError = {
        error: 'Too Many Requests',
        message: 'Rate limit exceeded',
        code: 'RATE_LIMIT_EXCEEDED',
      }

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 429,
        json: () => Promise.resolve(apiError),
      })

      const { queryRAG } = await import('@/lib/api/gateway')

      await expect(queryRAG({ query: 'Test' }, 'test-token')).rejects.toMatchObject({
        code: 'RATE_LIMIT_EXCEEDED',
        status: 429,
      })
    })

    it('handles 503 service unavailable error', async () => {
      const apiError: APIError = {
        error: 'Service Unavailable',
        message: 'RAG service temporarily unavailable',
        code: 'SERVICE_UNAVAILABLE',
      }

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 503,
        json: () => Promise.resolve(apiError),
      })

      const { queryRAG } = await import('@/lib/api/gateway')

      await expect(queryRAG({ query: 'Test' }, 'test-token')).rejects.toMatchObject({
        code: 'SERVICE_UNAVAILABLE',
        status: 503,
      })
    })
  })

  describe('queryRAGStream (streaming)', () => {
    // Helper to create a mock reader with releaseLock
    function createMockReader(readImpl: () => Promise<{ done: boolean; value: Uint8Array | undefined }>) {
      return {
        read: vi.fn().mockImplementation(readImpl),
        releaseLock: vi.fn(),
      }
    }

    it('sends POST request with Accept: text/event-stream header', async () => {
      const mockReader = createMockReader(() =>
        Promise.resolve({ done: true, value: undefined })
      )
      const mockResponse = {
        ok: true,
        body: {
          getReader: () => mockReader,
        },
      }

      mockFetch.mockResolvedValueOnce(mockResponse)

      const { queryRAGStream } = await import('@/lib/api/gateway')
      const request: QueryRequest = { query: 'Test query', stream: true }

      // Consume the async iterator
      const chunks: StreamChunk[] = []
      for await (const chunk of queryRAGStream(request, 'test-token')) {
        chunks.push(chunk)
      }

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.kindlast.com/api/v1/query',
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
            'Authorization': 'Bearer test-token',
            'Accept': 'text/event-stream',
          }),
        })
      )
    })

    it('sets stream: true in request body', async () => {
      const mockReader = createMockReader(() =>
        Promise.resolve({ done: true, value: undefined })
      )
      const mockResponse = {
        ok: true,
        body: {
          getReader: () => mockReader,
        },
      }

      mockFetch.mockResolvedValueOnce(mockResponse)

      const { queryRAGStream } = await import('@/lib/api/gateway')
      const request: QueryRequest = { query: 'Test query' }

      for await (const _ of queryRAGStream(request, 'test-token')) {
        // consume
      }

      const callArgs = mockFetch.mock.calls[0]
      const body = JSON.parse(callArgs[1].body)
      expect(body.stream).toBe(true)
    })

    it('yields content chunks from SSE stream', async () => {
      const sseData = [
        'event: content\ndata: {"type":"content","text":"GDPR"}\n\n',
        'event: content\ndata: {"type":"content","text":" requires"}\n\n',
        'event: done\ndata: {"type":"done"}\n\n',
      ]

      const encoder = new TextEncoder()
      let callIndex = 0

      const mockReader = createMockReader(() => {
        if (callIndex < sseData.length) {
          const data = encoder.encode(sseData[callIndex])
          callIndex++
          return Promise.resolve({ done: false, value: data })
        }
        return Promise.resolve({ done: true, value: undefined })
      })

      const mockResponse = {
        ok: true,
        body: {
          getReader: () => mockReader,
        },
      }

      mockFetch.mockResolvedValueOnce(mockResponse)

      const { queryRAGStream } = await import('@/lib/api/gateway')
      const chunks: StreamChunk[] = []

      for await (const chunk of queryRAGStream({ query: 'Test' }, 'test-token')) {
        chunks.push(chunk)
      }

      expect(chunks).toHaveLength(3)
      expect(chunks[0]).toEqual({ type: 'content', text: 'GDPR' })
      expect(chunks[1]).toEqual({ type: 'content', text: ' requires' })
      expect(chunks[2]).toEqual({ type: 'done' })
    })

    it('yields citation chunks from SSE stream', async () => {
      const citation = {
        source: 'GDPR',
        title: 'Article 5',
        url: 'https://example.com',
        excerpt: 'Text',
        relevance: 0.9,
      }

      const sseData = [
        `event: citation\ndata: {"type":"citation","citation":${JSON.stringify(citation)}}\n\n`,
        'event: done\ndata: {"type":"done"}\n\n',
      ]

      const encoder = new TextEncoder()
      let callIndex = 0

      const mockReader = createMockReader(() => {
        if (callIndex < sseData.length) {
          const data = encoder.encode(sseData[callIndex])
          callIndex++
          return Promise.resolve({ done: false, value: data })
        }
        return Promise.resolve({ done: true, value: undefined })
      })

      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: { getReader: () => mockReader },
      })

      const { queryRAGStream } = await import('@/lib/api/gateway')
      const chunks: StreamChunk[] = []

      for await (const chunk of queryRAGStream({ query: 'Test' }, 'test-token')) {
        chunks.push(chunk)
      }

      expect(chunks[0]).toEqual({ type: 'citation', citation })
    })

    it('yields error chunks from SSE stream', async () => {
      const sseData = [
        'event: error\ndata: {"type":"error","error":"Query processing failed"}\n\n',
      ]

      const encoder = new TextEncoder()
      let callIndex = 0

      const mockReader = createMockReader(() => {
        if (callIndex < sseData.length) {
          const data = encoder.encode(sseData[callIndex])
          callIndex++
          return Promise.resolve({ done: false, value: data })
        }
        return Promise.resolve({ done: true, value: undefined })
      })

      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: { getReader: () => mockReader },
      })

      const { queryRAGStream } = await import('@/lib/api/gateway')
      const chunks: StreamChunk[] = []

      for await (const chunk of queryRAGStream({ query: 'Test' }, 'test-token')) {
        chunks.push(chunk)
      }

      expect(chunks[0]).toEqual({ type: 'error', error: 'Query processing failed' })
    })

    it('handles metadata chunks', async () => {
      const sseData = [
        'event: metadata\ndata: {"type":"metadata","metadata":{"confidenceOk":true,"maxRelevance":0.92,"citationCount":3}}\n\n',
        'event: done\ndata: {"type":"done"}\n\n',
      ]

      const encoder = new TextEncoder()
      let callIndex = 0

      const mockReader = createMockReader(() => {
        if (callIndex < sseData.length) {
          const data = encoder.encode(sseData[callIndex])
          callIndex++
          return Promise.resolve({ done: false, value: data })
        }
        return Promise.resolve({ done: true, value: undefined })
      })

      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: { getReader: () => mockReader },
      })

      const { queryRAGStream } = await import('@/lib/api/gateway')
      const chunks: StreamChunk[] = []

      for await (const chunk of queryRAGStream({ query: 'Test' }, 'test-token')) {
        chunks.push(chunk)
      }

      expect(chunks[0]).toEqual({
        type: 'metadata',
        metadata: {
          confidenceOk: true,
          maxRelevance: 0.92,
          citationCount: 3,
        },
      })
    })

    it('throws GatewayError on HTTP error response', async () => {
      const apiError: APIError = {
        error: 'Unauthorized',
        message: 'Invalid token',
        code: 'INVALID_TOKEN',
      }

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () => Promise.resolve(apiError),
      })

      const { queryRAGStream, GatewayError } = await import('@/lib/api/gateway')

      const iterator = queryRAGStream({ query: 'Test' }, 'invalid-token')

      await expect(iterator.next()).rejects.toThrow(GatewayError)
    })

    it('throws error when response body is null', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: null,
      })

      const { queryRAGStream, GatewayError } = await import('@/lib/api/gateway')

      const iterator = queryRAGStream({ query: 'Test' }, 'test-token')

      await expect(iterator.next()).rejects.toThrow('Response body is null')
    })
  })

  describe('GatewayError', () => {
    it('has correct properties', async () => {
      const { GatewayError } = await import('@/lib/api/gateway')

      const error = new GatewayError('Test error', 'BAD_REQUEST', 400)

      expect(error.message).toBe('Test error')
      expect(error.code).toBe('BAD_REQUEST')
      expect(error.status).toBe(400)
      expect(error.name).toBe('GatewayError')
      expect(error instanceof Error).toBe(true)
    })

    it('isRetryable returns true for 503 errors', async () => {
      const { GatewayError } = await import('@/lib/api/gateway')

      const error = new GatewayError('Service unavailable', 'SERVICE_UNAVAILABLE', 503)

      expect(error.isRetryable()).toBe(true)
    })

    it('isRetryable returns true for 429 errors', async () => {
      const { GatewayError } = await import('@/lib/api/gateway')

      const error = new GatewayError('Rate limited', 'RATE_LIMIT_EXCEEDED', 429)

      expect(error.isRetryable()).toBe(true)
    })

    it('isRetryable returns false for 400 errors', async () => {
      const { GatewayError } = await import('@/lib/api/gateway')

      const error = new GatewayError('Bad request', 'BAD_REQUEST', 400)

      expect(error.isRetryable()).toBe(false)
    })

    it('isAuthError returns true for 401 errors', async () => {
      const { GatewayError } = await import('@/lib/api/gateway')

      const error = new GatewayError('Unauthorized', 'INVALID_TOKEN', 401)

      expect(error.isAuthError()).toBe(true)
    })
  })

  describe('parseSSEEvent', () => {
    it('parses valid SSE event', async () => {
      const { parseSSEEvent } = await import('@/lib/api/gateway')

      const raw = 'event: content\ndata: {"type":"content","text":"Hello"}\n\n'
      const result = parseSSEEvent(raw)

      expect(result).toEqual({ type: 'content', text: 'Hello' })
    })

    it('returns null for empty string', async () => {
      const { parseSSEEvent } = await import('@/lib/api/gateway')

      expect(parseSSEEvent('')).toBeNull()
    })

    it('returns null for invalid JSON data', async () => {
      const { parseSSEEvent } = await import('@/lib/api/gateway')

      const raw = 'event: content\ndata: invalid json\n\n'
      expect(parseSSEEvent(raw)).toBeNull()
    })

    it('handles multi-line data', async () => {
      const { parseSSEEvent } = await import('@/lib/api/gateway')

      const raw = 'event: content\ndata: {"type":"content"}\ndata: \n\n'
      const result = parseSSEEvent(raw)

      // Should parse just the first valid data line
      expect(result).toEqual({ type: 'content' })
    })
  })
})
