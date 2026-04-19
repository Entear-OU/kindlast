export interface BusinessProfile {
  id: string
  user_id: string
  company_name: string
  country: string
  industry: string | null
  employee_count: number | null
  processes_personal_data: boolean
  data_types: string[] | null
  uses_ai_systems: boolean
  ai_system_descriptions: Record<string, unknown>[] | null
  third_party_processors: string[] | null
  transfers_data_outside_eu: boolean
  has_dpo: boolean
  has_privacy_policy: boolean
  has_cookie_consent: boolean
  has_breach_notification: boolean
  has_dsr_process: boolean
  created_at: string
  updated_at: string
}

export interface Assessment {
  id: string
  user_id: string
  profile_id: string | null
  type: 'gdpr' | 'ai_act'
  status: 'pending' | 'processing' | 'complete' | 'error'
  overall_score: number | null
  risk_level: string | null
  result: Record<string, unknown> | null
  created_at: string
}

export interface Finding {
  id: string
  assessment_id: string
  user_id: string
  category: string
  severity: 'critical' | 'high' | 'medium' | 'low' | 'pass'
  title: string
  description: string
  recommendation: string
  gdpr_article: string | null
  ai_act_article: string | null
  is_resolved: boolean
  resolved_at: string | null
  created_at: string
}

export interface Subscription {
  id: string
  user_id: string
  stripe_customer_id: string | null
  stripe_subscription_id: string | null
  plan: 'free' | 'premium'
  status: string
  current_period_end: string | null
  created_at: string
}

// DPO Copilot types

export interface Client {
  id: string
  user_id: string
  name: string
  description: string | null
  sector: string | null
  country: string | null
  employee_count: number | null
  tech_stack: string[]
  data_subjects: string[]
  processing_purposes: string[]
  status: 'active' | 'archived'
  created_at: string
  updated_at: string
}

export type ArtifactType =
  | 'ropa'
  | 'dpia_screening'
  | 'dpa_gap'
  | 'lawful_basis'
  | 'ai_act_classification'

export type ArtifactStatus = 'draft' | 'reviewed' | 'approved' | 'exported'

export interface ArtifactCitation {
  index: number
  source_url: string
  title: string
  section: string
  chunk_text: string
}

export interface ArtifactGenerationMeta {
  provider: string
  model: string
  tokens_used: number
  latency_ms: number
  corpus_version: string
}

export interface Artifact {
  id: string
  client_id: string
  user_id: string
  type: ArtifactType
  status: ArtifactStatus
  title: string | null
  input_context: string
  generated_content: Record<string, unknown>
  edited_content: Record<string, unknown> | null
  citations: ArtifactCitation[]
  generation_meta: ArtifactGenerationMeta
  version: number
  created_at: string
  updated_at: string
}

export interface ArtifactAuditLog {
  id: string
  artifact_id: string
  user_id: string
  action: 'generated' | 'edited' | 'status_changed' | 'exported' | 'deleted'
  previous_state: Record<string, unknown> | null
  new_state: Record<string, unknown> | null
  metadata: {
    ip?: string
    user_agent?: string
    reason?: string
  }
  created_at: string
}
