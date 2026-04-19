import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock next/headers
const mockCookieStore = {
  get: vi.fn(),
  set: vi.fn(),
  delete: vi.fn(),
  getAll: vi.fn(() => []),
};

vi.mock('next/headers', () => ({
  cookies: vi.fn(() => Promise.resolve(mockCookieStore)),
}));

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch;

describe('Gateway API Client', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-01-15T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  describe('gatewayFetch', () => {
    it('makes request with JSON body and content-type header', async () => {
      const { gatewayFetch } = await import('@/lib/api/client');

      mockCookieStore.get.mockReturnValue(undefined);
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ success: true }),
      });

      await gatewayFetch('/api/v1/test', {
        method: 'POST',
        body: { data: 'test' },
        skipAuth: true,
      });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/test'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ data: 'test' }),
          headers: expect.any(Headers),
        })
      );

      const calledHeaders = mockFetch.mock.calls[0][1].headers;
      expect(calledHeaders.get('Content-Type')).toBe('application/json');
    });

    it('attaches JWT token to request when available', async () => {
      const { gatewayFetch } = await import('@/lib/api/client');

      // Valid token that expires in 2 hours (exp: 1705327200 = 2024-01-15T14:00:00Z)
      const validToken =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMjcyMDB9.test';

      mockCookieStore.get.mockReturnValueOnce({ value: validToken });
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ data: 'response' }),
      });

      await gatewayFetch('/api/v1/protected');

      const calledHeaders = mockFetch.mock.calls[0][1].headers;
      expect(calledHeaders.get('Authorization')).toBe(`Bearer ${validToken}`);
    });

    it('skips auth when skipAuth is true', async () => {
      const { gatewayFetch } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ status: 'ok' }),
      });

      await gatewayFetch('/health', { skipAuth: true });

      // Should not try to get token
      expect(mockCookieStore.get).not.toHaveBeenCalled();

      const calledHeaders = mockFetch.mock.calls[0][1].headers;
      expect(calledHeaders.get('Authorization')).toBeNull();
    });

    it('returns parsed JSON response', async () => {
      const { gatewayFetch } = await import('@/lib/api/client');

      const responseData = { id: '123', name: 'Test' };
      mockCookieStore.get.mockReturnValue(undefined);
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve(responseData),
      });

      const response = await gatewayFetch<typeof responseData>('/api/v1/resource', {
        skipAuth: true,
      });

      expect(response.data).toEqual(responseData);
      expect(response.status).toBe(200);
    });

    it('returns text response for non-JSON content type', async () => {
      const { gatewayFetch } = await import('@/lib/api/client');

      mockCookieStore.get.mockReturnValue(undefined);
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/plain' }),
        text: () => Promise.resolve('Plain text response'),
      });

      const response = await gatewayFetch<string>('/api/v1/text', { skipAuth: true });

      expect(response.data).toBe('Plain text response');
    });

    it('throws GatewayApiError on non-2xx response', async () => {
      const { gatewayFetch, GatewayApiError } = await import('@/lib/api/client');

      mockCookieStore.get.mockReturnValue(undefined);
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        json: () =>
          Promise.resolve({
            error: 'Resource not found',
            code: 'NOT_FOUND',
          }),
      });

      await expect(gatewayFetch('/api/v1/missing', { skipAuth: true })).rejects.toThrow(
        GatewayApiError
      );

      try {
        await gatewayFetch('/api/v1/missing', { skipAuth: true });
      } catch (error) {
        if (error instanceof GatewayApiError) {
          expect(error.message).toBe('Resource not found');
          expect(error.status).toBe(404);
          expect(error.code).toBe('NOT_FOUND');
        }
      }
    });

    it('throws GatewayAuthError on 401 response', async () => {
      const { gatewayFetch } = await import('@/lib/api/client');
      const { GatewayAuthError } = await import('@/lib/api/auth');

      mockCookieStore.get.mockReturnValue(undefined);
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () =>
          Promise.resolve({
            error: 'Token expired',
            code: 'TOKEN_EXPIRED',
          }),
      });

      await expect(gatewayFetch('/api/v1/protected', { skipAuth: true })).rejects.toThrow(
        GatewayAuthError
      );
    });

    it('handles timeout', async () => {
      const { gatewayFetch, GatewayApiError } = await import('@/lib/api/client');

      mockCookieStore.get.mockReturnValue(undefined);

      // Create a fetch that never resolves
      mockFetch.mockImplementationOnce(
        () =>
          new Promise((_, reject) => {
            // Simulate abort after timeout
            setTimeout(() => {
              const abortError = new Error('Aborted');
              abortError.name = 'AbortError';
              reject(abortError);
            }, 100);
          })
      );

      const fetchPromise = gatewayFetch('/api/v1/slow', {
        skipAuth: true,
        timeout: 50,
      });

      // Advance timers to trigger abort
      vi.advanceTimersByTime(100);

      await expect(fetchPromise).rejects.toThrow(GatewayApiError);
    });
  });

  describe('gateway convenience methods', () => {
    beforeEach(() => {
      mockCookieStore.get.mockReturnValue(undefined);
    });

    it('gateway.get makes GET request', async () => {
      const { gateway } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ items: [] }),
      });

      await gateway.get('/api/v1/items', { skipAuth: true });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/items'),
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('gateway.post makes POST request with body', async () => {
      const { gateway } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 201,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ id: 'new-123' }),
      });

      await gateway.post('/api/v1/items', { name: 'New Item' }, { skipAuth: true });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/items'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ name: 'New Item' }),
        })
      );
    });

    it('gateway.put makes PUT request with body', async () => {
      const { gateway } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ id: '123', name: 'Updated' }),
      });

      await gateway.put('/api/v1/items/123', { name: 'Updated' }, { skipAuth: true });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/items/123'),
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify({ name: 'Updated' }),
        })
      );
    });

    it('gateway.patch makes PATCH request with body', async () => {
      const { gateway } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ id: '123', status: 'active' }),
      });

      await gateway.patch('/api/v1/items/123', { status: 'active' }, { skipAuth: true });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/items/123'),
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ status: 'active' }),
        })
      );
    });

    it('gateway.delete makes DELETE request', async () => {
      const { gateway } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        headers: new Headers(),
        text: () => Promise.resolve(''),
      });

      await gateway.delete('/api/v1/items/123', { skipAuth: true });

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/items/123'),
        expect.objectContaining({ method: 'DELETE' })
      );
    });
  });

  describe('checkGatewayHealth', () => {
    it('returns true when gateway is healthy', async () => {
      const { checkGatewayHealth } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve({ status: 'ok' }),
      });

      const isHealthy = await checkGatewayHealth();

      expect(isHealthy).toBe(true);
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/health'),
        expect.any(Object)
      );
    });

    it('returns false when gateway returns error', async () => {
      const { checkGatewayHealth } = await import('@/lib/api/client');

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 503,
        json: () => Promise.resolve({ error: 'Service unavailable' }),
      });

      const isHealthy = await checkGatewayHealth();

      expect(isHealthy).toBe(false);
    });

    it('returns false when fetch fails', async () => {
      const { checkGatewayHealth } = await import('@/lib/api/client');

      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      const isHealthy = await checkGatewayHealth();

      expect(isHealthy).toBe(false);
    });
  });
});

describe('GatewayApiError', () => {
  it('creates error with all properties', async () => {
    const { GatewayApiError } = await import('@/lib/api/client');

    const error = new GatewayApiError('Not found', 404, 'NOT_FOUND', { details: 'Resource missing' });

    expect(error.message).toBe('Not found');
    expect(error.status).toBe(404);
    expect(error.code).toBe('NOT_FOUND');
    expect(error.data).toEqual({ details: 'Resource missing' });
    expect(error.name).toBe('GatewayApiError');
  });
});
