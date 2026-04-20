/**
 * Gateway API queries for SME Assessment flow.
 * Replaces Supabase queries with direct Gateway API calls.
 */

import { gateway } from './client';
import { API_ENDPOINTS } from './config';
import type {
  BusinessProfile,
  Assessment,
  Finding,
} from '@/lib/types/database';

// ============================================================================
// Response Types
// ============================================================================

export interface PaginatedResponse<T> {
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface AssessmentListResponse extends PaginatedResponse<Assessment> {
  assessments: Assessment[];
}

export interface FindingListResponse extends PaginatedResponse<Finding> {
  findings: Finding[];
}

// ============================================================================
// Profile Queries
// ============================================================================

/**
 * Get the current user's business profile.
 * Returns null if no profile exists.
 */
export async function getBusinessProfile(): Promise<BusinessProfile | null> {
  try {
    const response = await gateway.get<BusinessProfile>(API_ENDPOINTS.profile);
    return response.data;
  } catch (error) {
    // 404 means no profile exists yet
    if (error instanceof Error && 'status' in error && (error as { status: number }).status === 404) {
      return null;
    }
    throw error;
  }
}

/**
 * Create or update the current user's business profile.
 */
export async function saveBusinessProfile(
  profile: Partial<BusinessProfile>
): Promise<BusinessProfile> {
  const response = await gateway.post<BusinessProfile>(API_ENDPOINTS.profile, profile);
  return response.data;
}

// ============================================================================
// Assessment Queries
// ============================================================================

/**
 * Get the latest assessment for the current user.
 * Returns null if no assessments exist.
 */
export async function getLatestAssessment(): Promise<Assessment | null> {
  try {
    const response = await gateway.get<Assessment>(API_ENDPOINTS.assessments.latest);
    return response.data;
  } catch (error) {
    // 404 means no assessments exist yet
    if (error instanceof Error && 'status' in error && (error as { status: number }).status === 404) {
      return null;
    }
    throw error;
  }
}

/**
 * Get a specific assessment by ID.
 */
export async function getAssessment(id: string): Promise<Assessment | null> {
  try {
    const response = await gateway.get<Assessment>(API_ENDPOINTS.assessments.get(id));
    return response.data;
  } catch (error) {
    if (error instanceof Error && 'status' in error && (error as { status: number }).status === 404) {
      return null;
    }
    throw error;
  }
}

/**
 * List all assessments for the current user.
 */
export async function listAssessments(
  page: number = 1,
  pageSize: number = 20
): Promise<AssessmentListResponse> {
  const response = await gateway.get<AssessmentListResponse>(
    `${API_ENDPOINTS.assessments.list}?page=${page}&page_size=${pageSize}`
  );
  return response.data;
}

/**
 * Create a new assessment.
 */
export async function createAssessment(
  type: 'gdpr' | 'ai_act'
): Promise<Assessment> {
  const response = await gateway.post<Assessment>(API_ENDPOINTS.assessments.create, { type });
  return response.data;
}

/**
 * Update an assessment (status, score, result, etc.)
 */
export async function updateAssessment(
  id: string,
  data: Partial<Pick<Assessment, 'status' | 'overall_score' | 'risk_level' | 'result'>>
): Promise<void> {
  await gateway.patch(API_ENDPOINTS.assessments.update(id), data);
}

// ============================================================================
// Finding Queries
// ============================================================================

/**
 * Get findings for a specific assessment.
 */
export async function getFindings(
  assessmentId: string,
  page: number = 1,
  pageSize: number = 50
): Promise<FindingListResponse> {
  const response = await gateway.get<FindingListResponse>(
    `${API_ENDPOINTS.assessments.findings(assessmentId)}?page=${page}&page_size=${pageSize}`
  );
  return response.data;
}

/**
 * Get all findings for the current user.
 */
export async function getUserFindings(
  page: number = 1,
  pageSize: number = 50,
  resolved?: boolean
): Promise<FindingListResponse> {
  let url = `${API_ENDPOINTS.findings.list}?page=${page}&page_size=${pageSize}`;
  if (resolved !== undefined) {
    url += `&resolved=${resolved}`;
  }
  const response = await gateway.get<FindingListResponse>(url);
  return response.data;
}

/**
 * Update a finding (mark as resolved/unresolved).
 */
export async function updateFinding(
  id: string,
  isResolved: boolean
): Promise<Finding> {
  const response = await gateway.patch<Finding>(
    API_ENDPOINTS.findings.update(id),
    { is_resolved: isResolved }
  );
  return response.data;
}

// ============================================================================
// User Plan Query
// ============================================================================

export interface UserPlan {
  plan: string;
  status: string;
  current_period_end?: string;
  features?: string[];
}

/**
 * Get the current user's subscription plan.
 */
export async function getUserPlan(): Promise<UserPlan> {
  const response = await gateway.get<UserPlan>(API_ENDPOINTS.users.plan);
  return response.data;
}
