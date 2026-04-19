import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

describe('API Config Module', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    vi.resetModules();
    process.env = { ...originalEnv };
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  describe('getApiConfig', () => {
    it('returns default config when no env vars set', async () => {
      delete process.env.NEXT_PUBLIC_API_URL;
      delete process.env.NEXT_PUBLIC_API_TIMEOUT;
      delete process.env.NEXT_PUBLIC_TOKEN_EXPIRY_BUFFER;
      delete process.env.GATEWAY_ACCESS_TOKEN_COOKIE;
      delete process.env.GATEWAY_REFRESH_TOKEN_COOKIE;

      const { getApiConfig } = await import('@/lib/api/config');
      const config = getApiConfig();

      expect(config.baseUrl).toBe('http://localhost:8080');
      expect(config.timeout).toBe(30000);
      expect(config.tokenExpiryBuffer).toBe(60000);
      expect(config.accessTokenCookie).toBe('gateway_access_token');
      expect(config.refreshTokenCookie).toBe('gateway_refresh_token');
    });

    it('uses environment variables when set', async () => {
      process.env.NEXT_PUBLIC_API_URL = 'https://api.kindlast.com';
      process.env.NEXT_PUBLIC_API_TIMEOUT = '60000';
      process.env.NEXT_PUBLIC_TOKEN_EXPIRY_BUFFER = '120000';
      process.env.GATEWAY_ACCESS_TOKEN_COOKIE = 'custom_access';
      process.env.GATEWAY_REFRESH_TOKEN_COOKIE = 'custom_refresh';

      const { getApiConfig } = await import('@/lib/api/config');
      const config = getApiConfig();

      expect(config.baseUrl).toBe('https://api.kindlast.com');
      expect(config.timeout).toBe(60000);
      expect(config.tokenExpiryBuffer).toBe(120000);
      expect(config.accessTokenCookie).toBe('custom_access');
      expect(config.refreshTokenCookie).toBe('custom_refresh');
    });
  });

  describe('API_ENDPOINTS', () => {
    it('defines auth endpoints', async () => {
      const { API_ENDPOINTS } = await import('@/lib/api/config');

      expect(API_ENDPOINTS.auth.login).toBe('/api/v1/auth/login');
      expect(API_ENDPOINTS.auth.register).toBe('/api/v1/auth/register');
      expect(API_ENDPOINTS.auth.refresh).toBe('/api/v1/auth/refresh');
      expect(API_ENDPOINTS.auth.me).toBe('/api/v1/auth/me');
      expect(API_ENDPOINTS.auth.exchange).toBe('/api/v1/auth/exchange');
    });

    it('defines rag endpoints', async () => {
      const { API_ENDPOINTS } = await import('@/lib/api/config');

      expect(API_ENDPOINTS.rag.query).toBe('/api/v1/rag/query');
      expect(API_ENDPOINTS.rag.search).toBe('/api/v1/rag/search');
    });

    it('defines health endpoint', async () => {
      const { API_ENDPOINTS } = await import('@/lib/api/config');

      expect(API_ENDPOINTS.health).toBe('/health');
    });
  });

  describe('buildApiUrl', () => {
    it('builds URL from base and endpoint', async () => {
      const { buildApiUrl } = await import('@/lib/api/config');

      const url = buildApiUrl('/api/v1/auth/login', {
        baseUrl: 'https://api.example.com',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access',
        refreshTokenCookie: 'refresh',
      });

      expect(url).toBe('https://api.example.com/api/v1/auth/login');
    });

    it('handles trailing slash in base URL', async () => {
      const { buildApiUrl } = await import('@/lib/api/config');

      const url = buildApiUrl('/api/v1/health', {
        baseUrl: 'https://api.example.com/',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access',
        refreshTokenCookie: 'refresh',
      });

      expect(url).toBe('https://api.example.com/api/v1/health');
    });

    it('handles endpoint without leading slash', async () => {
      const { buildApiUrl } = await import('@/lib/api/config');

      const url = buildApiUrl('health', {
        baseUrl: 'https://api.example.com',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access',
        refreshTokenCookie: 'refresh',
      });

      expect(url).toBe('https://api.example.com/health');
    });

    it('uses default config when not provided', async () => {
      process.env.NEXT_PUBLIC_API_URL = 'https://default.api.com';

      const { buildApiUrl } = await import('@/lib/api/config');

      const url = buildApiUrl('/test');

      expect(url).toBe('https://default.api.com/test');
    });
  });

  describe('validateApiConfig', () => {
    it('returns valid for correct config', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: 'https://api.example.com',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access_token',
        refreshTokenCookie: 'refresh_token',
      });

      expect(result.valid).toBe(true);
      expect(result.errors).toHaveLength(0);
    });

    it('returns errors for missing baseUrl', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: '',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access_token',
        refreshTokenCookie: 'refresh_token',
      });

      expect(result.valid).toBe(false);
      expect(result.errors).toContain('baseUrl is required');
    });

    it('returns errors for invalid baseUrl', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: 'not-a-valid-url',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access_token',
        refreshTokenCookie: 'refresh_token',
      });

      expect(result.valid).toBe(false);
      expect(result.errors).toContain('baseUrl must be a valid URL');
    });

    it('returns errors for non-positive timeout', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: 'https://api.example.com',
        timeout: 0,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: 'access_token',
        refreshTokenCookie: 'refresh_token',
      });

      expect(result.valid).toBe(false);
      expect(result.errors).toContain('timeout must be a positive number');
    });

    it('returns errors for negative tokenExpiryBuffer', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: 'https://api.example.com',
        timeout: 30000,
        tokenExpiryBuffer: -1,
        accessTokenCookie: 'access_token',
        refreshTokenCookie: 'refresh_token',
      });

      expect(result.valid).toBe(false);
      expect(result.errors).toContain('tokenExpiryBuffer must be non-negative');
    });

    it('returns errors for missing cookie names', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: 'https://api.example.com',
        timeout: 30000,
        tokenExpiryBuffer: 60000,
        accessTokenCookie: '',
        refreshTokenCookie: '',
      });

      expect(result.valid).toBe(false);
      expect(result.errors).toContain('accessTokenCookie is required');
      expect(result.errors).toContain('refreshTokenCookie is required');
    });

    it('returns multiple errors at once', async () => {
      const { validateApiConfig } = await import('@/lib/api/config');

      const result = validateApiConfig({
        baseUrl: '',
        timeout: -1,
        tokenExpiryBuffer: -1,
        accessTokenCookie: '',
        refreshTokenCookie: '',
      });

      expect(result.valid).toBe(false);
      expect(result.errors.length).toBeGreaterThan(1);
    });
  });
});
