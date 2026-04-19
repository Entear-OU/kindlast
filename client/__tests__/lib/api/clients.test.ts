import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  listClients,
  getClient,
  createClient,
  updateClient,
  archiveClient,
  listArtifacts,
  getArtifact,
  generateArtifact,
  updateArtifact,
  updateArtifactStatus,
  isPlanLimitError,
  isNotFoundError,
  isAuthorizationError,
} from '@/lib/api/clients'
import { gateway, GatewayApiError } from '@/lib/api/client'
import type { Client, Artifact } from '@/lib/types/database'

// Mock the gateway module
vi.mock('@/lib/api/client', () => ({
  gateway: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
  GatewayApiError: class GatewayApiError extends Error {
    constructor(
      message: string,
      public readonly status: number,
      public readonly code?: string
    ) {
      super(message)
      this.name = 'GatewayApiError'
    }
  },
}))

const mockClient: Client = {
  id: 'client-1',
  user_id: 'user-1',
  name: 'Test Client',
  description: 'Test description',
  sector: 'saas',
  country: 'DE',
  employee_count: 50,
  tech_stack: ['Stripe'],
  data_subjects: ['Customers'],
  processing_purposes: ['Payment Processing'],
  status: 'active',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

const mockArtifact: Artifact = {
  id: 'artifact-1',
  client_id: 'client-1',
  user_id: 'user-1',
  type: 'ropa',
  status: 'draft',
  title: 'Test RoPA',
  input_context: 'Test context',
  generated_content: {},
  edited_content: null,
  citations: [],
  generation_meta: {
    provider: 'anthropic',
    model: 'claude-3-sonnet',
    tokens_used: 1000,
    latency_ms: 5000,
    corpus_version: '2024-01',
  },
  version: 1,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

// Helper to create mock response
function mockGatewayResponse<T>(data: T) {
  return {
    data,
    status: 200,
    headers: new Headers(),
  }
}

describe('clients API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('listClients', () => {
    it('fetches clients without filters', async () => {
      const mockResponse = mockGatewayResponse({
        clients: [mockClient],
        total: 1,
        page: 1,
        limit: 20,
      })
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      const result = await listClients()

      expect(gateway.get).toHaveBeenCalledWith('/api/v1/clients')
      expect(result.clients).toHaveLength(1)
      expect(result.clients[0].name).toBe('Test Client')
    })

    it('fetches clients with status filter', async () => {
      const mockResponse = mockGatewayResponse({
        clients: [mockClient],
        total: 1,
        page: 1,
        limit: 20,
      })
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      await listClients({ status: 'active' })

      expect(gateway.get).toHaveBeenCalledWith('/api/v1/clients?status=active')
    })

    it('fetches clients with pagination', async () => {
      const mockResponse = mockGatewayResponse({
        clients: [mockClient],
        total: 50,
        page: 2,
        limit: 10,
      })
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      await listClients({ page: 2, limit: 10 })

      expect(gateway.get).toHaveBeenCalledWith('/api/v1/clients?page=2&limit=10')
    })
  })

  describe('getClient', () => {
    it('fetches a single client by ID', async () => {
      const mockResponse = mockGatewayResponse(mockClient)
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      const result = await getClient('client-1')

      expect(gateway.get).toHaveBeenCalledWith('/api/v1/clients/client-1')
      expect(result.name).toBe('Test Client')
    })
  })

  describe('createClient', () => {
    it('creates a new client', async () => {
      const mockResponse = mockGatewayResponse(mockClient)
      vi.mocked(gateway.post).mockResolvedValue(mockResponse)

      const createData = {
        name: 'Test Client',
        description: 'Test description',
        sector: 'saas',
      }

      const result = await createClient(createData)

      expect(gateway.post).toHaveBeenCalledWith('/api/v1/clients', createData)
      expect(result.name).toBe('Test Client')
    })
  })

  describe('updateClient', () => {
    it('updates an existing client', async () => {
      const updatedClient = { ...mockClient, name: 'Updated Name' }
      const mockResponse = mockGatewayResponse(updatedClient)
      vi.mocked(gateway.put).mockResolvedValue(mockResponse)

      const result = await updateClient('client-1', { name: 'Updated Name' })

      expect(gateway.put).toHaveBeenCalledWith('/api/v1/clients/client-1', {
        name: 'Updated Name',
      })
      expect(result.name).toBe('Updated Name')
    })
  })

  describe('archiveClient', () => {
    it('archives a client', async () => {
      vi.mocked(gateway.delete).mockResolvedValue(mockGatewayResponse(undefined))

      await archiveClient('client-1')

      expect(gateway.delete).toHaveBeenCalledWith('/api/v1/clients/client-1')
    })
  })

  describe('listArtifacts', () => {
    it('fetches artifacts for a client', async () => {
      const mockResponse = mockGatewayResponse({
        artifacts: [mockArtifact],
        total: 1,
        page: 1,
        limit: 20,
      })
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      const result = await listArtifacts('client-1')

      expect(gateway.get).toHaveBeenCalledWith('/api/v1/clients/client-1/artifacts')
      expect(result.artifacts).toHaveLength(1)
    })

    it('fetches artifacts with filters', async () => {
      const mockResponse = mockGatewayResponse({
        artifacts: [mockArtifact],
        total: 1,
        page: 1,
        limit: 20,
      })
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      await listArtifacts('client-1', { type: 'ropa', status: 'draft' })

      expect(gateway.get).toHaveBeenCalledWith(
        '/api/v1/clients/client-1/artifacts?type=ropa&status=draft'
      )
    })
  })

  describe('getArtifact', () => {
    it('fetches a single artifact', async () => {
      const mockResponse = mockGatewayResponse(mockArtifact)
      vi.mocked(gateway.get).mockResolvedValue(mockResponse)

      const result = await getArtifact('client-1', 'artifact-1')

      expect(gateway.get).toHaveBeenCalledWith(
        '/api/v1/clients/client-1/artifacts/artifact-1'
      )
      expect(result.type).toBe('ropa')
    })
  })

  describe('generateArtifact', () => {
    it('generates a new artifact', async () => {
      const mockResponse = mockGatewayResponse(mockArtifact)
      vi.mocked(gateway.post).mockResolvedValue(mockResponse)

      const result = await generateArtifact('client-1', {
        type: 'ropa',
        input_context: 'Test context',
      })

      expect(gateway.post).toHaveBeenCalledWith(
        '/api/v1/clients/client-1/artifacts/generate',
        { type: 'ropa', input_context: 'Test context' }
      )
      expect(result.type).toBe('ropa')
    })
  })

  describe('updateArtifact', () => {
    it('updates an artifact', async () => {
      const mockResponse = mockGatewayResponse({ ...mockArtifact, title: 'New Title' })
      vi.mocked(gateway.put).mockResolvedValue(mockResponse)

      const result = await updateArtifact('client-1', 'artifact-1', {
        title: 'New Title',
      })

      expect(gateway.put).toHaveBeenCalledWith(
        '/api/v1/clients/client-1/artifacts/artifact-1',
        { title: 'New Title' }
      )
      expect(result.title).toBe('New Title')
    })
  })

  describe('updateArtifactStatus', () => {
    it('updates artifact status', async () => {
      const mockResponse = mockGatewayResponse({ ...mockArtifact, status: 'approved' as const })
      vi.mocked(gateway.put).mockResolvedValue(mockResponse)

      const result = await updateArtifactStatus('client-1', 'artifact-1', {
        status: 'approved',
      })

      expect(gateway.put).toHaveBeenCalledWith(
        '/api/v1/clients/client-1/artifacts/artifact-1/status',
        { status: 'approved' }
      )
      expect(result.status).toBe('approved')
    })
  })

  describe('error helpers', () => {
    it('isPlanLimitError returns true for 429 status', () => {
      const error = new GatewayApiError('Plan limit exceeded', 429)
      expect(isPlanLimitError(error)).toBe(true)
    })

    it('isPlanLimitError returns true for QUOTA_EXCEEDED code', () => {
      const error = new GatewayApiError('Quota exceeded', 403, 'QUOTA_EXCEEDED')
      expect(isPlanLimitError(error)).toBe(true)
    })

    it('isPlanLimitError returns false for other errors', () => {
      const error = new GatewayApiError('Not found', 404)
      expect(isPlanLimitError(error)).toBe(false)
    })

    it('isNotFoundError returns true for 404 status', () => {
      const error = new GatewayApiError('Not found', 404)
      expect(isNotFoundError(error)).toBe(true)
    })

    it('isNotFoundError returns false for other status', () => {
      const error = new GatewayApiError('Server error', 500)
      expect(isNotFoundError(error)).toBe(false)
    })

    it('isAuthorizationError returns true for 403 status', () => {
      const error = new GatewayApiError('Forbidden', 403)
      expect(isAuthorizationError(error)).toBe(true)
    })

    it('isAuthorizationError returns false for other status', () => {
      const error = new GatewayApiError('Not found', 404)
      expect(isAuthorizationError(error)).toBe(false)
    })

    it('error helpers return false for non-GatewayApiError', () => {
      const error = new Error('Regular error')
      expect(isPlanLimitError(error)).toBe(false)
      expect(isNotFoundError(error)).toBe(false)
      expect(isAuthorizationError(error)).toBe(false)
    })
  })
})
