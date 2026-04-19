/**
 * Gateway API module exports.
 *
 * This module provides everything needed to communicate with the backend gateway.
 */

// Configuration
export {
  type ApiConfig,
  getApiConfig,
  API_ENDPOINTS,
  buildApiUrl,
  validateApiConfig,
} from './config';

// Auth token management
export {
  type GatewayUser,
  type GatewayAuthResponse,
  GatewayAuthError,
  parseJwtExpiry,
  isTokenExpired,
  exchangeSupabaseToken,
  refreshGatewayToken,
  storeGatewayTokens,
  getGatewayToken,
  getGatewayRefreshToken,
  clearGatewayTokens,
  getValidGatewayToken,
  authenticateWithGateway,
} from './auth';

// API client
export {
  type GatewayRequestOptions,
  type GatewayResponse,
  GatewayApiError,
  gatewayFetch,
  gateway,
  checkGatewayHealth,
} from './client';
