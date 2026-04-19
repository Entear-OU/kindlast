/**
 * TypeScript types for the Kindlast Gateway API
 *
 * These types match the Go backend API contracts defined in:
 * - services/gateway/internal/models/models.go
 * - services/rag/internal/rag/orchestrator.go
 * - services/rag/internal/prompts/templates.go
 */

// ============================================================================
// Authentication Types
// ============================================================================

export interface RegisterRequest {
  email: string
  password: string
  full_name?: string
}

export interface LoginRequest {
  email: string
  password: string
}

export interface RefreshRequest {
  refresh_token: string
}

export interface UserProfile {
  id: string
  email: string
  full_name: string
  plan: UserPlan
  created_at: string
}

export interface AuthResponse {
  access_token: string
  refresh_token: string
  user: UserProfile
}

export type UserPlan = 'free' | 'professional' | 'team'

// ============================================================================
// Query Types (RAG Service)
// ============================================================================

export type Topic = 'gdpr' | 'ai_act' | 'both'

export interface QueryRequest {
  query: string
  topic?: Topic
  topK?: number
  stream?: boolean
}

export interface Citation {
  source: string
  title: string
  url: string
  excerpt: string
  relevance: number
}

export interface QueryResponse {
  answer: string
  citations: Citation[]
  cacheHit: boolean
  confidenceOk: boolean
  maxRelevance: number
  processingTime: number // nanoseconds in Go, converted to milliseconds
}

// ============================================================================
// Streaming Types
// ============================================================================

export type StreamChunkType = 'content' | 'citation' | 'error' | 'done' | 'metadata'

export interface StreamChunk {
  type: StreamChunkType
  text?: string
  citation?: Citation
  error?: string
  metadata?: StreamMetadata
}

export interface StreamMetadata {
  confidenceOk: boolean
  maxRelevance: number
  citationCount: number
}

// ============================================================================
// Error Types
// ============================================================================

export interface APIError {
  error: string
  message: string
  code: ErrorCode
}

export type ErrorCode =
  | 'BAD_REQUEST'
  | 'VALIDATION_ERROR'
  | 'UNAUTHORIZED'
  | 'INVALID_CREDENTIALS'
  | 'INVALID_TOKEN'
  | 'USER_NOT_FOUND'
  | 'EMAIL_EXISTS'
  | 'INTERNAL_ERROR'
  | 'SERVICE_UNAVAILABLE'
  | 'SERVICE_ERROR'
  | 'RATE_LIMIT_EXCEEDED'
  | 'QUOTA_EXCEEDED'

// ============================================================================
// Plan Types
// ============================================================================

export interface PlanDetails {
  plan: UserPlan
  queries_per_month: number
  queries_used: number
  rate_limit_per_min: number
}

export interface PlanLimit {
  requests_per_hour: number
  max_citations: number
  queries_per_month: number
  rate_limit_per_min: number
}

// ============================================================================
// Health Types
// ============================================================================

export interface HealthResponse {
  status: 'healthy' | 'degraded'
  version?: string
  components: Record<string, string>
  timestamp: string
}

export interface ProviderStatus {
  generation: ProviderInfo
  embedding: ProviderInfo
  reranking: ProviderInfo
  retrieval: {
    vector_db: string
    healthy: boolean
  }
  cache: {
    backend: string
    healthy: boolean
  }
}

export interface ProviderInfo {
  primary: string
  fallback: string
  healthy: boolean
}

// ============================================================================
// User Types
// ============================================================================

export interface UpdateProfileRequest {
  full_name?: string
}
