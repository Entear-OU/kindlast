import { z } from 'zod'
import type {
  QueryRequest,
  QueryResponse,
  StreamChunk,
  APIError,
  ErrorCode,
} from './types'

/**
 * Gateway client for communicating with the backend RAG service.
 *
 * This module provides two APIs:
 * 1. New API: queryRAG(request, token) and queryRAGStream(request, token)
 *    - Uses typed request/response based on backend API contracts
 *    - Supports both streaming (SSE) and non-streaming responses
 *
 * 2. Legacy API: queryRAGWithSchema(options)
 *    - Uses Zod schemas for structured responses
 *    - Maintained for backward compatibility
 */

// ============================================================================
// Configuration
// ============================================================================

// Legacy configuration (for backward compatibility)
// Use internal URL for server-side requests (Docker network)
// Falls back to NEXT_PUBLIC_API_URL for local development without Docker
const isServer = typeof window === 'undefined'
const GATEWAY_URL = isServer
  ? (process.env.API_URL_INTERNAL || process.env.GATEWAY_URL || process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080')
  : (process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080')
const GATEWAY_API_KEY = process.env.GATEWAY_API_KEY || ''

/**
 * Gets the API base URL from environment variable.
 * @throws Error if NEXT_PUBLIC_API_URL is not set
 */
export function getApiUrl(): string {
  const url = process.env.NEXT_PUBLIC_API_URL

  if (!url) {
    throw new Error('NEXT_PUBLIC_API_URL environment variable is not set')
  }

  // Remove trailing slash if present
  return url.replace(/\/$/, '')
}

// ============================================================================
// Legacy Types (for backward compatibility)
// ============================================================================

export interface LegacyCitation {
  source: string
  title: string
  url?: string
  article?: string
  excerpt: string
  relevance_score: number
}

export interface RAGResponse<T> {
  data: T
  citations: LegacyCitation[]
  model: string
  usage: {
    prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
}

export interface QueryRAGOptions<T extends z.ZodTypeAny> {
  query: string
  systemPrompt: string
  schema: T
  collection?: string
  topK?: number
  temperature?: number
  /** JWT token for authentication (optional, falls back to GATEWAY_API_KEY) */
  token?: string
}

// ============================================================================
// Error Classes
// ============================================================================

/**
 * Custom error class for API gateway errors.
 * Provides typed error information and helpers for error handling.
 */
export class GatewayError extends Error {
  public readonly code: ErrorCode | string
  public readonly status: number

  // Legacy property for backward compatibility
  public readonly statusCode: number

  constructor(message: string, code: ErrorCode | string, status: number) {
    super(message)
    this.name = 'GatewayError'
    this.code = code
    this.status = status
    this.statusCode = status // Backward compatibility

    // Maintains proper stack trace for where error was thrown
    if (Error.captureStackTrace) {
      Error.captureStackTrace(this, GatewayError)
    }
  }

  /**
   * Returns true if the error is potentially recoverable by retrying.
   * Includes rate limits and temporary service unavailability.
   */
  isRetryable(): boolean {
    return this.status === 429 || this.status === 503 || this.status === 502
  }

  /**
   * Returns true if the error is an authentication error.
   */
  isAuthError(): boolean {
    return this.status === 401 || this.code === 'INVALID_TOKEN' || this.code === 'UNAUTHORIZED'
  }
}

// ============================================================================
// Request Helpers
// ============================================================================

/**
 * Creates headers for API requests.
 */
function createHeaders(token: string, streaming: boolean = false): HeadersInit {
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  }

  if (streaming) {
    headers['Accept'] = 'text/event-stream'
  }

  return headers
}

/**
 * Handles error responses from the API.
 */
async function handleErrorResponse(response: Response): Promise<never> {
  let apiError: APIError

  try {
    apiError = await response.json()
  } catch {
    // If response body is not valid JSON, create a generic error
    apiError = {
      error: response.statusText,
      message: `HTTP ${response.status}: ${response.statusText}`,
      code: 'INTERNAL_ERROR',
    }
  }

  throw new GatewayError(
    apiError.message || apiError.error,
    apiError.code || 'INTERNAL_ERROR',
    response.status
  )
}

// ============================================================================
// RAG Query Functions (New API)
// ============================================================================

/**
 * Sends a RAG query request and returns the complete response.
 *
 * @param request - The query request parameters
 * @param token - JWT access token for authentication
 * @returns The complete query response with answer and citations
 * @throws GatewayError on API errors
 *
 * @example
 * ```typescript
 * const response = await queryRAG(
 *   { query: 'What is GDPR Article 5?', topic: 'gdpr' },
 *   accessToken
 * )
 * console.log(response.answer)
 * console.log(response.citations)
 * ```
 */
export async function queryRAG(
  request: QueryRequest,
  token: string
): Promise<QueryResponse> {
  const baseUrl = getApiUrl()
  const url = `${baseUrl}/api/v1/query`

  // Ensure stream is false for non-streaming requests
  const body: QueryRequest = {
    ...request,
    stream: false,
  }

  let response: Response

  try {
    response = await fetch(url, {
      method: 'POST',
      headers: createHeaders(token, false),
      body: JSON.stringify(body),
    })
  } catch (error) {
    // Network error or fetch failed
    throw new GatewayError(
      error instanceof Error ? error.message : 'Network request failed',
      'SERVICE_ERROR',
      0
    )
  }

  if (!response.ok) {
    await handleErrorResponse(response)
  }

  return response.json()
}

// ============================================================================
// SSE Parsing
// ============================================================================

/**
 * Parses a Server-Sent Event string into a StreamChunk.
 *
 * @param raw - Raw SSE event string (e.g., "event: content\ndata: {...}\n\n")
 * @returns Parsed StreamChunk or null if parsing fails
 */
export function parseSSEEvent(raw: string): StreamChunk | null {
  if (!raw || raw.trim() === '') {
    return null
  }

  // SSE format: "event: <type>\ndata: <json>\n\n"
  const lines = raw.split('\n')
  let dataLine: string | null = null

  for (const line of lines) {
    if (line.startsWith('data: ')) {
      dataLine = line.slice(6)
      break
    }
  }

  if (!dataLine) {
    return null
  }

  try {
    return JSON.parse(dataLine) as StreamChunk
  } catch {
    return null
  }
}

// ============================================================================
// Streaming Query
// ============================================================================

/**
 * Sends a RAG query request and streams the response as Server-Sent Events.
 *
 * @param request - The query request parameters
 * @param token - JWT access token for authentication
 * @yields StreamChunk objects for each SSE event (content, citation, metadata, error, done)
 * @throws GatewayError on API errors
 *
 * @example
 * ```typescript
 * for await (const chunk of queryRAGStream({ query: 'What is GDPR?' }, token)) {
 *   switch (chunk.type) {
 *     case 'content':
 *       process.stdout.write(chunk.text || '')
 *       break
 *     case 'citation':
 *       console.log('Citation:', chunk.citation)
 *       break
 *     case 'error':
 *       console.error('Error:', chunk.error)
 *       break
 *     case 'done':
 *       console.log('Stream complete')
 *       break
 *   }
 * }
 * ```
 */
export async function* queryRAGStream(
  request: QueryRequest,
  token: string
): AsyncGenerator<StreamChunk, void, unknown> {
  const baseUrl = getApiUrl()
  const url = `${baseUrl}/api/v1/query`

  // Ensure stream is true for streaming requests
  const body: QueryRequest = {
    ...request,
    stream: true,
  }

  let response: Response

  try {
    response = await fetch(url, {
      method: 'POST',
      headers: createHeaders(token, true),
      body: JSON.stringify(body),
    })
  } catch (error) {
    throw new GatewayError(
      error instanceof Error ? error.message : 'Network request failed',
      'SERVICE_ERROR',
      0
    )
  }

  if (!response.ok) {
    await handleErrorResponse(response)
  }

  if (!response.body) {
    throw new GatewayError('Response body is null', 'SERVICE_ERROR', 0)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()

      if (done) {
        break
      }

      // Decode the chunk and add to buffer
      buffer += decoder.decode(value, { stream: true })

      // Process complete SSE events (separated by double newline)
      const events = buffer.split('\n\n')

      // Keep the last potentially incomplete event in the buffer
      buffer = events.pop() || ''

      for (const event of events) {
        if (event.trim()) {
          const chunk = parseSSEEvent(event + '\n\n')
          if (chunk) {
            yield chunk

            // Stop iteration if we receive done or error
            if (chunk.type === 'done') {
              return
            }
          }
        }
      }
    }

    // Process any remaining data in buffer
    if (buffer.trim()) {
      const chunk = parseSSEEvent(buffer + '\n\n')
      if (chunk) {
        yield chunk
      }
    }
  } finally {
    reader.releaseLock()
  }
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Type guard to check if an error is a GatewayError.
 */
export function isGatewayError(error: unknown): error is GatewayError {
  return error instanceof GatewayError
}

/**
 * Extracts a user-friendly error message from any error.
 */
export function getErrorMessage(error: unknown): string {
  if (isGatewayError(error)) {
    return error.message
  }

  if (error instanceof Error) {
    return error.message
  }

  return 'An unexpected error occurred'
}

// ============================================================================
// Legacy Functions (for backward compatibility)
// ============================================================================

/**
 * Converts a Zod schema to JSON Schema format for the gateway API.
 * This is a simplified implementation for common schema types.
 */
function zodSchemaToJsonSchema(schema: z.ZodTypeAny): Record<string, unknown> {
  // For now, return a simple representation
  // The actual conversion happens server-side when using zod-to-json-schema
  // This is a placeholder that the gateway can interpret
  return {
    type: 'object',
    _zodSchema: true,
    description: schema.description,
  }
}

/**
 * @deprecated Use queryRAG(request, token) instead
 *
 * Queries the RAG service with a prompt and returns a structured response.
 *
 * @param options - Query options including prompt, schema, and RAG parameters
 * @returns Structured response with data and citations
 */
export async function queryRAGWithSchema<T extends z.ZodTypeAny>(
  options: QueryRAGOptions<T>
): Promise<RAGResponse<z.infer<T>>> {
  const { query, systemPrompt, schema, collection = 'regulatory', topK = 5, temperature = 0.3, token } = options

  // Use provided token first, fall back to GATEWAY_API_KEY
  const authToken = token || GATEWAY_API_KEY

  const response = await fetch(`${GATEWAY_URL}/api/v1/query`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(authToken && { Authorization: `Bearer ${authToken}` }),
    },
    body: JSON.stringify({
      query,
      system_prompt: systemPrompt,
      response_schema: zodSchemaToJsonSchema(schema),
      collection,
      top_k: topK,
      temperature,
    }),
  })

  if (!response.ok) {
    const errorBody = await response.text()
    let errorMessage = 'Gateway request failed'
    let errorCode = 'GATEWAY_ERROR'

    try {
      const errorJson = JSON.parse(errorBody)
      errorMessage = errorJson.message || errorMessage
      errorCode = errorJson.code || errorCode
    } catch {
      // Use default error message if parsing fails
    }

    throw new GatewayError(errorMessage, errorCode, response.status)
  }

  const result = await response.json()

  // Validate response data against schema
  const validatedData = schema.parse(result.data)

  return {
    data: validatedData,
    citations: result.citations || [],
    model: result.model || 'unknown',
    usage: result.usage || {
      prompt_tokens: 0,
      completion_tokens: 0,
      total_tokens: 0,
    },
  }
}

/**
 * Health check for the gateway service.
 */
export async function checkGatewayHealth(): Promise<boolean> {
  try {
    const response = await fetch(`${GATEWAY_URL}/health`, {
      method: 'GET',
      headers: {
        ...(GATEWAY_API_KEY && { Authorization: `Bearer ${GATEWAY_API_KEY}` }),
      },
    })
    return response.ok
  } catch {
    return false
  }
}

/**
 * Gets the current gateway configuration.
 */
export function getGatewayConfig() {
  return {
    url: GATEWAY_URL,
    hasApiKey: Boolean(GATEWAY_API_KEY),
  }
}
