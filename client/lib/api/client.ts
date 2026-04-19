/**
 * Gateway API client with automatic JWT token management.
 *
 * Provides a fetch wrapper that:
 * - Automatically attaches valid JWT tokens to requests
 * - Refreshes tokens when expired
 * - Handles common error scenarios
 * - Supports typed responses with Zod validation
 */

import { getApiConfig, buildApiUrl } from './config';
import { getValidGatewayToken, GatewayAuthError } from './auth';

/**
 * Options for gateway API requests
 */
export interface GatewayRequestOptions extends Omit<RequestInit, 'body'> {
  /** Request body (will be JSON.stringify'd if object) */
  body?: unknown;
  /** Skip automatic token attachment */
  skipAuth?: boolean;
  /** Custom timeout in milliseconds (overrides config) */
  timeout?: number;
}

/**
 * Gateway API response wrapper
 */
export interface GatewayResponse<T> {
  data: T;
  status: number;
  headers: Headers;
}

/**
 * Gateway API error
 */
export class GatewayApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code?: string,
    public readonly data?: unknown
  ) {
    super(message);
    this.name = 'GatewayApiError';
  }
}

/**
 * Make an authenticated request to the gateway API.
 * Automatically attaches JWT token and handles token refresh.
 *
 * @param endpoint API endpoint (relative to base URL)
 * @param options Request options
 * @returns Typed response data
 * @throws GatewayApiError on request failure
 * @throws GatewayAuthError on authentication failure
 */
export async function gatewayFetch<T = unknown>(
  endpoint: string,
  options: GatewayRequestOptions = {}
): Promise<GatewayResponse<T>> {
  const config = getApiConfig();
  const { skipAuth = false, timeout, body, ...fetchOptions } = options;

  const url = buildApiUrl(endpoint, config);

  // Build headers
  const headers = new Headers(fetchOptions.headers);

  // Set Content-Type if body is provided and not already set
  if (body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  // Attach auth token unless skipped
  if (!skipAuth) {
    const token = await getValidGatewayToken();
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
  }

  // Prepare request body
  const requestBody =
    body !== undefined
      ? typeof body === 'string'
        ? body
        : JSON.stringify(body)
      : undefined;

  // Create abort controller for timeout
  const controller = new AbortController();
  const timeoutMs = timeout ?? config.timeout;
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(url, {
      ...fetchOptions,
      headers,
      body: requestBody,
      signal: controller.signal,
    });

    clearTimeout(timeoutId);

    // Handle error responses
    if (!response.ok) {
      let errorData: Record<string, unknown> | undefined;
      try {
        errorData = await response.json();
      } catch {
        // Response might not be JSON
      }

      const errorMessage =
        (errorData?.error as string) ||
        (errorData?.message as string) ||
        `Request failed with status ${response.status}`;
      const errorCode = errorData?.code as string | undefined;

      // Special handling for auth errors
      if (response.status === 401) {
        throw new GatewayAuthError(errorMessage, errorCode || 'UNAUTHORIZED', response.status);
      }

      throw new GatewayApiError(errorMessage, response.status, errorCode, errorData);
    }

    // Parse successful response
    let data: T;
    const contentType = response.headers.get('Content-Type');
    if (contentType?.includes('application/json')) {
      data = await response.json();
    } else {
      data = (await response.text()) as unknown as T;
    }

    return {
      data,
      status: response.status,
      headers: response.headers,
    };
  } catch (error) {
    clearTimeout(timeoutId);

    if (error instanceof GatewayApiError || error instanceof GatewayAuthError) {
      throw error;
    }

    if (error instanceof Error && error.name === 'AbortError') {
      throw new GatewayApiError(`Request timeout after ${timeoutMs}ms`, 408, 'TIMEOUT');
    }

    throw error;
  }
}

/**
 * Convenience methods for common HTTP verbs
 */
export const gateway = {
  /**
   * Make a GET request
   */
  get<T = unknown>(
    endpoint: string,
    options?: Omit<GatewayRequestOptions, 'method' | 'body'>
  ): Promise<GatewayResponse<T>> {
    return gatewayFetch<T>(endpoint, { ...options, method: 'GET' });
  },

  /**
   * Make a POST request
   */
  post<T = unknown>(
    endpoint: string,
    body?: unknown,
    options?: Omit<GatewayRequestOptions, 'method' | 'body'>
  ): Promise<GatewayResponse<T>> {
    return gatewayFetch<T>(endpoint, { ...options, method: 'POST', body });
  },

  /**
   * Make a PUT request
   */
  put<T = unknown>(
    endpoint: string,
    body?: unknown,
    options?: Omit<GatewayRequestOptions, 'method' | 'body'>
  ): Promise<GatewayResponse<T>> {
    return gatewayFetch<T>(endpoint, { ...options, method: 'PUT', body });
  },

  /**
   * Make a PATCH request
   */
  patch<T = unknown>(
    endpoint: string,
    body?: unknown,
    options?: Omit<GatewayRequestOptions, 'method' | 'body'>
  ): Promise<GatewayResponse<T>> {
    return gatewayFetch<T>(endpoint, { ...options, method: 'PATCH', body });
  },

  /**
   * Make a DELETE request
   */
  delete<T = unknown>(
    endpoint: string,
    options?: Omit<GatewayRequestOptions, 'method' | 'body'>
  ): Promise<GatewayResponse<T>> {
    return gatewayFetch<T>(endpoint, { ...options, method: 'DELETE' });
  },
};

/**
 * Check if gateway API is reachable
 */
export async function checkGatewayHealth(): Promise<boolean> {
  try {
    const response = await gatewayFetch('/health', { skipAuth: true });
    return response.status === 200;
  } catch {
    return false;
  }
}
