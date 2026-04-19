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

describe('Gateway Auth Module', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-01-15T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  describe('exchangeSupabaseToken', () => {
    it('exchanges Supabase session for gateway tokens', async () => {
      const { exchangeSupabaseToken } = await import('@/lib/api/auth');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            access_token: 'gateway_access_token',
            refresh_token: 'gateway_refresh_token',
            user: {
              id: 'user-123',
              email: 'test@example.com',
              plan: 'free',
            },
          }),
      });

      const result = await exchangeSupabaseToken('supabase_jwt_token');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/auth/exchange'),
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
          }),
          body: JSON.stringify({ supabase_token: 'supabase_jwt_token' }),
        })
      );

      expect(result).toEqual({
        accessToken: 'gateway_access_token',
        refreshToken: 'gateway_refresh_token',
        user: {
          id: 'user-123',
          email: 'test@example.com',
          plan: 'free',
        },
      });
    });

    it('throws error when exchange fails', async () => {
      const { exchangeSupabaseToken } = await import('@/lib/api/auth');

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () =>
          Promise.resolve({
            error: 'Invalid Supabase token',
            code: 'INVALID_TOKEN',
          }),
      });

      await expect(exchangeSupabaseToken('invalid_token')).rejects.toThrow(
        'Failed to exchange token: Invalid Supabase token'
      );
    });

    it('throws error on network failure', async () => {
      const { exchangeSupabaseToken } = await import('@/lib/api/auth');

      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      await expect(exchangeSupabaseToken('token')).rejects.toThrow('Network error');
    });
  });

  describe('refreshGatewayToken', () => {
    it('refreshes gateway access token using refresh token', async () => {
      const { refreshGatewayToken } = await import('@/lib/api/auth');

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            access_token: 'new_access_token',
            refresh_token: 'new_refresh_token',
            user: {
              id: 'user-123',
              email: 'test@example.com',
              plan: 'pro',
            },
          }),
      });

      const result = await refreshGatewayToken('old_refresh_token');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/auth/refresh'),
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
          }),
          body: JSON.stringify({ refresh_token: 'old_refresh_token' }),
        })
      );

      expect(result).toEqual({
        accessToken: 'new_access_token',
        refreshToken: 'new_refresh_token',
        user: {
          id: 'user-123',
          email: 'test@example.com',
          plan: 'pro',
        },
      });
    });

    it('throws error when refresh fails', async () => {
      const { refreshGatewayToken } = await import('@/lib/api/auth');

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () =>
          Promise.resolve({
            error: 'Invalid or expired refresh token',
            code: 'INVALID_TOKEN',
          }),
      });

      await expect(refreshGatewayToken('expired_refresh_token')).rejects.toThrow(
        'Failed to refresh token: Invalid or expired refresh token'
      );
    });
  });

  describe('parseJwtExpiry', () => {
    it('parses expiry time from JWT token', async () => {
      const { parseJwtExpiry } = await import('@/lib/api/auth');

      // JWT with exp: 1705326000 (2024-01-15T13:00:00Z)
      const token =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoidXNlci0xMjMiLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJwbGFuIjoiZnJlZSIsImV4cCI6MTcwNTMyNjAwMCwiaWF0IjoxNzA1MzIyNDAwfQ.test_signature';

      const expiry = parseJwtExpiry(token);

      expect(expiry).toBeInstanceOf(Date);
      expect(expiry?.getTime()).toBe(1705326000000);
    });

    it('returns null for invalid token', async () => {
      const { parseJwtExpiry } = await import('@/lib/api/auth');

      expect(parseJwtExpiry('invalid_token')).toBeNull();
      expect(parseJwtExpiry('')).toBeNull();
    });

    it('returns null for token without exp claim', async () => {
      const { parseJwtExpiry } = await import('@/lib/api/auth');

      // JWT without exp claim
      const token =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoidXNlci0xMjMifQ.test_signature';

      expect(parseJwtExpiry(token)).toBeNull();
    });
  });

  describe('isTokenExpired', () => {
    it('returns true for expired token', async () => {
      const { isTokenExpired } = await import('@/lib/api/auth');

      // Token expired 1 hour ago (exp: 1705316400 = 2024-01-15T11:00:00Z)
      const expiredToken =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMTY0MDB9.test';

      expect(isTokenExpired(expiredToken)).toBe(true);
    });

    it('returns false for valid token', async () => {
      const { isTokenExpired } = await import('@/lib/api/auth');

      // Token expires in 1 hour (exp: 1705323600 = 2024-01-15T13:00:00Z)
      const validToken =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMjM2MDB9.test';

      expect(isTokenExpired(validToken)).toBe(false);
    });

    it('returns true when token is about to expire within buffer', async () => {
      vi.resetModules();
      const { isTokenExpired } = await import('@/lib/api/auth');

      // Token expires in 30 seconds (less than default 60s buffer)
      // exp: 1705320030 = 2024-01-15T12:00:30Z
      // Current time: 1705320000 = 2024-01-15T12:00:00Z
      // With 60s buffer, should be considered expired since 30s < 60s
      const soonToExpireToken =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMjAwMzB9.test';

      expect(isTokenExpired(soonToExpireToken)).toBe(true);
    });

    it('respects custom buffer', async () => {
      const { isTokenExpired } = await import('@/lib/api/auth');

      // Token expires in 30 seconds (exp: 1705320030 = 2024-01-15T12:00:30Z)
      const soonToExpireToken =
        'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMjAwMzB9.test';

      // With 10s buffer, token should not be considered expired (30s > 10s)
      expect(isTokenExpired(soonToExpireToken, 10000)).toBe(false);
    });

    it('returns true for invalid token', async () => {
      const { isTokenExpired } = await import('@/lib/api/auth');

      expect(isTokenExpired('invalid_token')).toBe(true);
      expect(isTokenExpired('')).toBe(true);
    });
  });

  describe('GatewayAuthError', () => {
    it('creates error with code and status', async () => {
      const { GatewayAuthError } = await import('@/lib/api/auth');

      const error = new GatewayAuthError('Token expired', 'TOKEN_EXPIRED', 401);

      expect(error.message).toBe('Token expired');
      expect(error.code).toBe('TOKEN_EXPIRED');
      expect(error.status).toBe(401);
      expect(error.name).toBe('GatewayAuthError');
    });
  });
});

describe('Gateway Auth Token Storage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('storeGatewayTokens', () => {
    it('stores tokens in cookies with secure options', async () => {
      const { storeGatewayTokens } = await import('@/lib/api/auth');

      await storeGatewayTokens('access_token', 'refresh_token');

      expect(mockCookieStore.set).toHaveBeenCalledTimes(2);
      expect(mockCookieStore.set).toHaveBeenCalledWith(
        'gateway_access_token',
        'access_token',
        expect.objectContaining({
          httpOnly: true,
          secure: expect.any(Boolean),
          sameSite: 'lax',
          path: '/',
        })
      );
      expect(mockCookieStore.set).toHaveBeenCalledWith(
        'gateway_refresh_token',
        'refresh_token',
        expect.objectContaining({
          httpOnly: true,
          secure: expect.any(Boolean),
          sameSite: 'lax',
          path: '/',
        })
      );
    });
  });

  describe('getGatewayToken', () => {
    it('returns access token from cookie', async () => {
      const { getGatewayToken } = await import('@/lib/api/auth');

      mockCookieStore.get.mockReturnValueOnce({ value: 'stored_access_token' });

      const token = await getGatewayToken();

      expect(mockCookieStore.get).toHaveBeenCalledWith('gateway_access_token');
      expect(token).toBe('stored_access_token');
    });

    it('returns null when no token stored', async () => {
      const { getGatewayToken } = await import('@/lib/api/auth');

      mockCookieStore.get.mockReturnValueOnce(undefined);

      const token = await getGatewayToken();

      expect(token).toBeNull();
    });
  });

  describe('getGatewayRefreshToken', () => {
    it('returns refresh token from cookie', async () => {
      const { getGatewayRefreshToken } = await import('@/lib/api/auth');

      mockCookieStore.get.mockReturnValueOnce({ value: 'stored_refresh_token' });

      const token = await getGatewayRefreshToken();

      expect(mockCookieStore.get).toHaveBeenCalledWith('gateway_refresh_token');
      expect(token).toBe('stored_refresh_token');
    });
  });

  describe('clearGatewayTokens', () => {
    it('removes both tokens from cookies', async () => {
      const { clearGatewayTokens } = await import('@/lib/api/auth');

      await clearGatewayTokens();

      expect(mockCookieStore.delete).toHaveBeenCalledTimes(2);
      expect(mockCookieStore.delete).toHaveBeenCalledWith('gateway_access_token');
      expect(mockCookieStore.delete).toHaveBeenCalledWith('gateway_refresh_token');
    });
  });
});

describe('Gateway Auth - getValidGatewayToken', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-01-15T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns valid token without refresh', async () => {
    const { getValidGatewayToken } = await import('@/lib/api/auth');

    // Token expires in 2 hours (exp: 1705327200 = 2024-01-15T14:00:00Z)
    const validToken =
      'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMjcyMDB9.test';

    mockCookieStore.get.mockReturnValueOnce({ value: validToken });

    const token = await getValidGatewayToken();

    expect(token).toBe(validToken);
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('refreshes token when expired and returns new token', async () => {
    const { getValidGatewayToken } = await import('@/lib/api/auth');

    // Expired token (exp: 1705316400 = 2024-01-15T11:00:00Z)
    const expiredToken =
      'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMTY0MDB9.test';
    const refreshToken = 'valid_refresh_token';
    const newAccessToken = 'new_access_token';

    mockCookieStore.get
      .mockReturnValueOnce({ value: expiredToken }) // First call for access token
      .mockReturnValueOnce({ value: refreshToken }); // Second call for refresh token

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () =>
        Promise.resolve({
          access_token: newAccessToken,
          refresh_token: 'new_refresh_token',
          user: { id: 'user-123', email: 'test@example.com', plan: 'free' },
        }),
    });

    const token = await getValidGatewayToken();

    expect(token).toBe(newAccessToken);
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/auth/refresh'),
      expect.any(Object)
    );
  });

  it('returns null when no token and no refresh token', async () => {
    const { getValidGatewayToken } = await import('@/lib/api/auth');

    mockCookieStore.get.mockReturnValue(undefined);

    const token = await getValidGatewayToken();

    expect(token).toBeNull();
  });

  it('returns null when refresh fails', async () => {
    const { getValidGatewayToken } = await import('@/lib/api/auth');

    // Expired token (exp: 1705316400 = 2024-01-15T11:00:00Z)
    const expiredToken =
      'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDUzMTY0MDB9.test';
    const refreshToken = 'invalid_refresh_token';

    mockCookieStore.get
      .mockReturnValueOnce({ value: expiredToken })
      .mockReturnValueOnce({ value: refreshToken });

    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 401,
      json: () => Promise.resolve({ error: 'Invalid refresh token' }),
    });

    const token = await getValidGatewayToken();

    expect(token).toBeNull();
  });
});
