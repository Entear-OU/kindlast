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
  type LoginRequest,
  type RegisterRequest,
  GatewayAuthError,
  parseJwtExpiry,
  isTokenExpired,
  login,
  register,
  loginAndStore,
  registerAndStore,
  getCurrentUser,
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

// Data queries (replaces Supabase queries)
export {
  type PaginatedResponse,
  type AssessmentListResponse,
  type FindingListResponse,
  type UserPlan,
  getBusinessProfile,
  saveBusinessProfile,
  getLatestAssessment,
  getAssessment,
  listAssessments,
  createAssessment,
  updateAssessment,
  getFindings,
  getUserFindings,
  updateFinding,
  getUserPlan,
} from './queries';
