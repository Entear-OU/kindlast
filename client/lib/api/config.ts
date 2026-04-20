/**
 * Centralized API configuration for gateway communication.
 * Provides type-safe configuration with environment validation.
 */

export interface ApiConfig {
  /** Base URL for the gateway API */
  baseUrl: string;
  /** Timeout for API requests in milliseconds */
  timeout: number;
  /** Access token expiry buffer in milliseconds (refresh before actual expiry) */
  tokenExpiryBuffer: number;
  /** Cookie name for storing gateway access token */
  accessTokenCookie: string;
  /** Cookie name for storing gateway refresh token */
  refreshTokenCookie: string;
}

/**
 * Default configuration values
 */
const DEFAULT_CONFIG: ApiConfig = {
  baseUrl: 'http://localhost:8080',
  timeout: 30000, // 30 seconds
  tokenExpiryBuffer: 60000, // 1 minute before expiry
  accessTokenCookie: 'gateway_access_token',
  refreshTokenCookie: 'gateway_refresh_token',
};

/**
 * Get API configuration with environment overrides.
 * Uses internal URL for server-side requests (Docker network) and
 * public URL for client-side requests (browser).
 */
export function getApiConfig(): ApiConfig {
  // For server-side requests, use internal URL if available (Docker network)
  // For client-side requests, use public URL (browser access)
  const isServer = typeof window === 'undefined';
  const internalUrl = process.env.API_URL_INTERNAL;
  const publicUrl = process.env.NEXT_PUBLIC_API_URL || DEFAULT_CONFIG.baseUrl;

  // Use internal URL for server-side, public URL for client-side
  const baseUrl = isServer && internalUrl ? internalUrl : publicUrl;

  return {
    baseUrl,
    timeout: parseInt(process.env.NEXT_PUBLIC_API_TIMEOUT || String(DEFAULT_CONFIG.timeout), 10),
    tokenExpiryBuffer: parseInt(
      process.env.NEXT_PUBLIC_TOKEN_EXPIRY_BUFFER || String(DEFAULT_CONFIG.tokenExpiryBuffer),
      10
    ),
    accessTokenCookie: process.env.GATEWAY_ACCESS_TOKEN_COOKIE || DEFAULT_CONFIG.accessTokenCookie,
    refreshTokenCookie: process.env.GATEWAY_REFRESH_TOKEN_COOKIE || DEFAULT_CONFIG.refreshTokenCookie,
  };
}

/**
 * API endpoints for the gateway
 */
export const API_ENDPOINTS = {
  // Auth endpoints
  auth: {
    login: '/api/v1/auth/login',
    register: '/api/v1/auth/register',
    refresh: '/api/v1/auth/refresh',
    me: '/api/v1/auth/me',
    exchange: '/api/v1/auth/exchange', // Exchange Supabase token for gateway token
  },
  // User endpoints
  users: {
    me: '/api/v1/users/me',
    plan: '/api/v1/users/me/plan',
  },
  // RAG/Query endpoints
  rag: {
    query: '/api/v1/query',
    search: '/api/v1/rag/search',
  },
  // SME Assessment endpoints
  profile: '/api/v1/profile',
  assessments: {
    list: '/api/v1/assessments',
    create: '/api/v1/assessments',
    latest: '/api/v1/assessments/latest',
    get: (id: string) => `/api/v1/assessments/${id}`,
    update: (id: string) => `/api/v1/assessments/${id}`,
    findings: (id: string) => `/api/v1/assessments/${id}/findings`,
  },
  findings: {
    list: '/api/v1/findings',
    update: (id: string) => `/api/v1/findings/${id}`,
  },
  // DPO Copilot endpoints
  clients: {
    list: '/api/v1/clients',
    create: '/api/v1/clients',
    get: (id: string) => `/api/v1/clients/${id}`,
    update: (id: string) => `/api/v1/clients/${id}`,
    archive: (id: string) => `/api/v1/clients/${id}`,
  },
  artifacts: {
    list: (clientId: string) => `/api/v1/clients/${clientId}/artifacts`,
    generate: (clientId: string) => `/api/v1/clients/${clientId}/artifacts/generate`,
    get: (clientId: string, artifactId: string) => `/api/v1/clients/${clientId}/artifacts/${artifactId}`,
    update: (clientId: string, artifactId: string) => `/api/v1/clients/${clientId}/artifacts/${artifactId}`,
    updateStatus: (clientId: string, artifactId: string) => `/api/v1/clients/${clientId}/artifacts/${artifactId}/status`,
    audit: (clientId: string, artifactId: string) => `/api/v1/clients/${clientId}/artifacts/${artifactId}/audit`,
    export: (clientId: string, artifactId: string) => `/api/v1/clients/${clientId}/artifacts/${artifactId}/export`,
    versions: (clientId: string, artifactId: string) => `/api/v1/clients/${clientId}/artifacts/${artifactId}/versions`,
  },
  processors: {
    list: '/api/v1/processors',
    search: '/api/v1/processors/search',
    categories: '/api/v1/processors/categories',
    get: (slug: string) => `/api/v1/processors/${slug}`,
  },
  audit: {
    list: '/api/v1/audit',
    export: '/api/v1/audit/export',
    summary: '/api/v1/audit/summary',
  },
  // Health check
  health: '/health',
} as const;

/**
 * Build full URL for an API endpoint
 */
export function buildApiUrl(endpoint: string, config?: ApiConfig): string {
  const apiConfig = config || getApiConfig();
  const baseUrl = apiConfig.baseUrl.replace(/\/$/, ''); // Remove trailing slash
  const path = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;
  return `${baseUrl}${path}`;
}

/**
 * Validate API configuration
 */
export function validateApiConfig(config: ApiConfig): { valid: boolean; errors: string[] } {
  const errors: string[] = [];

  if (!config.baseUrl) {
    errors.push('baseUrl is required');
  } else {
    try {
      new URL(config.baseUrl);
    } catch {
      errors.push('baseUrl must be a valid URL');
    }
  }

  if (config.timeout <= 0) {
    errors.push('timeout must be a positive number');
  }

  if (config.tokenExpiryBuffer < 0) {
    errors.push('tokenExpiryBuffer must be non-negative');
  }

  if (!config.accessTokenCookie) {
    errors.push('accessTokenCookie is required');
  }

  if (!config.refreshTokenCookie) {
    errors.push('refreshTokenCookie is required');
  }

  return {
    valid: errors.length === 0,
    errors,
  };
}
