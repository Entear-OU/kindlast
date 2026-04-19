/**
 * API client functions for DPO Copilot client and artifact management.
 *
 * These functions interact with the gateway API for:
 * - Client CRUD operations
 * - Artifact generation and management
 */

import { gateway, GatewayApiError } from './client'
import type {
  Client,
  Artifact,
  ArtifactType,
  ArtifactStatus,
} from '@/lib/types/database'

// ============================================================================
// Client Types
// ============================================================================

export interface CreateClientRequest {
  name: string
  description?: string
  sector?: string
  country?: string
  employee_count?: number
  tech_stack?: string[]
  data_subjects?: string[]
  processing_purposes?: string[]
}

export interface UpdateClientRequest {
  name?: string
  description?: string
  sector?: string
  country?: string
  employee_count?: number
  tech_stack?: string[]
  data_subjects?: string[]
  processing_purposes?: string[]
  status?: 'active' | 'archived'
}

export interface ListClientsParams {
  status?: 'active' | 'archived'
  page?: number
  limit?: number
}

export interface ListClientsResponse {
  clients: Client[]
  total: number
  page: number
  limit: number
}

// ============================================================================
// Artifact Types
// ============================================================================

export interface GenerateArtifactRequest {
  type: ArtifactType
  input_context: string
}

export interface UpdateArtifactRequest {
  edited_content?: Record<string, unknown>
  title?: string
}

export interface UpdateArtifactStatusRequest {
  status: ArtifactStatus
  reason?: string
}

export interface ListArtifactsParams {
  type?: ArtifactType
  status?: ArtifactStatus
  page?: number
  limit?: number
}

export interface ListArtifactsResponse {
  artifacts: Artifact[]
  total: number
  page: number
  limit: number
}

// ============================================================================
// Client API Functions
// ============================================================================

/**
 * List all clients for the current user
 */
export async function listClients(
  params: ListClientsParams = {}
): Promise<ListClientsResponse> {
  const searchParams = new URLSearchParams()
  if (params.status) searchParams.set('status', params.status)
  if (params.page) searchParams.set('page', String(params.page))
  if (params.limit) searchParams.set('limit', String(params.limit))

  const query = searchParams.toString()
  const endpoint = `/api/v1/clients${query ? `?${query}` : ''}`

  const response = await gateway.get<ListClientsResponse>(endpoint)
  return response.data
}

/**
 * Get a single client by ID
 */
export async function getClient(clientId: string): Promise<Client> {
  const response = await gateway.get<Client>(`/api/v1/clients/${clientId}`)
  return response.data
}

/**
 * Create a new client
 */
export async function createClient(data: CreateClientRequest): Promise<Client> {
  const response = await gateway.post<Client>('/api/v1/clients', data)
  return response.data
}

/**
 * Update an existing client
 */
export async function updateClient(
  clientId: string,
  data: UpdateClientRequest
): Promise<Client> {
  const response = await gateway.put<Client>(`/api/v1/clients/${clientId}`, data)
  return response.data
}

/**
 * Archive a client (soft delete)
 */
export async function archiveClient(clientId: string): Promise<void> {
  await gateway.delete(`/api/v1/clients/${clientId}`)
}

// ============================================================================
// Artifact API Functions
// ============================================================================

/**
 * List artifacts for a client
 */
export async function listArtifacts(
  clientId: string,
  params: ListArtifactsParams = {}
): Promise<ListArtifactsResponse> {
  const searchParams = new URLSearchParams()
  if (params.type) searchParams.set('type', params.type)
  if (params.status) searchParams.set('status', params.status)
  if (params.page) searchParams.set('page', String(params.page))
  if (params.limit) searchParams.set('limit', String(params.limit))

  const query = searchParams.toString()
  const endpoint = `/api/v1/clients/${clientId}/artifacts${query ? `?${query}` : ''}`

  const response = await gateway.get<ListArtifactsResponse>(endpoint)
  return response.data
}

/**
 * Get a single artifact by ID
 */
export async function getArtifact(
  clientId: string,
  artifactId: string
): Promise<Artifact> {
  const response = await gateway.get<Artifact>(
    `/api/v1/clients/${clientId}/artifacts/${artifactId}`
  )
  return response.data
}

/**
 * Generate a new artifact for a client
 */
export async function generateArtifact(
  clientId: string,
  data: GenerateArtifactRequest
): Promise<Artifact> {
  const response = await gateway.post<Artifact>(
    `/api/v1/clients/${clientId}/artifacts/generate`,
    data
  )
  return response.data
}

/**
 * Update an artifact (DPO edits)
 */
export async function updateArtifact(
  clientId: string,
  artifactId: string,
  data: UpdateArtifactRequest
): Promise<Artifact> {
  const response = await gateway.put<Artifact>(
    `/api/v1/clients/${clientId}/artifacts/${artifactId}`,
    data
  )
  return response.data
}

/**
 * Update artifact status (draft -> reviewed -> approved)
 */
export async function updateArtifactStatus(
  clientId: string,
  artifactId: string,
  data: UpdateArtifactStatusRequest
): Promise<Artifact> {
  const response = await gateway.put<Artifact>(
    `/api/v1/clients/${clientId}/artifacts/${artifactId}/status`,
    data
  )
  return response.data
}

/**
 * Export artifact to PDF/DOCX
 */
export async function exportArtifact(
  clientId: string,
  artifactId: string,
  format: 'pdf' | 'docx' = 'pdf'
): Promise<Blob> {
  const response = await gateway.post<Blob>(
    `/api/v1/clients/${clientId}/artifacts/${artifactId}/export`,
    { format }
  )
  return response.data
}

// ============================================================================
// Error Handling Helpers
// ============================================================================

/**
 * Check if error is a plan limit error
 */
export function isPlanLimitError(error: unknown): boolean {
  return (
    error instanceof GatewayApiError &&
    (error.status === 429 || error.code === 'QUOTA_EXCEEDED')
  )
}

/**
 * Check if error is a not found error
 */
export function isNotFoundError(error: unknown): boolean {
  return error instanceof GatewayApiError && error.status === 404
}

/**
 * Check if error is an authorization error
 */
export function isAuthorizationError(error: unknown): boolean {
  return error instanceof GatewayApiError && error.status === 403
}
