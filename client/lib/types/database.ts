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
