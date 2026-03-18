import { z } from 'zod'

// Step 1: Company Basics
export const step1Schema = z.object({
  company_name: z.string().min(1, 'Company name is required'),
  country: z.string().default('Estonia'),
  industry: z.string().optional(),
  employee_count: z.number().optional(),
})

// Step 2: Data Processing
export const step2Schema = z.object({
  processes_personal_data: z.boolean(),
  data_types: z.array(z.string()),
  third_party_processors: z.array(z.string()),
  transfers_data_outside_eu: z.boolean(),
})

// Step 3: Current Compliance Measures
export const step3Schema = z.object({
  has_privacy_policy: z.boolean(),
  has_cookie_consent: z.boolean(),
  has_dpo: z.boolean(),
  has_breach_notification: z.boolean(),
  has_dsr_process: z.boolean(),
})

// AI System description object
export const aiSystemSchema = z.object({
  name: z.string(),
  purpose: z.string(),
  dataUsed: z.string(),
  isAutomatedDecision: z.boolean(),
})

// Step 4: AI Systems (conditional via union)
export const step4Schema = z.union([
  z.object({
    uses_ai_systems: z.literal(false),
    ai_system_descriptions: z.array(aiSystemSchema).optional(),
  }),
  z.object({
    uses_ai_systems: z.literal(true),
    ai_system_descriptions: z.array(aiSystemSchema).min(1, 'At least one AI system description is required'),
  }),
])

// Combined full profile schema
export const fullProfileSchema = z.object({
  // Step 1
  company_name: z.string().min(1, 'Company name is required'),
  country: z.string().default('Estonia'),
  industry: z.string().optional(),
  employee_count: z.number().optional(),
  // Step 2
  processes_personal_data: z.boolean(),
  data_types: z.array(z.string()),
  third_party_processors: z.array(z.string()),
  transfers_data_outside_eu: z.boolean(),
  // Step 3
  has_privacy_policy: z.boolean(),
  has_cookie_consent: z.boolean(),
  has_dpo: z.boolean(),
  has_breach_notification: z.boolean(),
  has_dsr_process: z.boolean(),
  // Step 4
  uses_ai_systems: z.boolean(),
  ai_system_descriptions: z.array(aiSystemSchema).optional(),
}).refine(
  (data) => {
    if (data.uses_ai_systems) {
      return data.ai_system_descriptions && data.ai_system_descriptions.length > 0
    }
    return true
  },
  { message: 'AI system descriptions are required when using AI systems' }
)

// Inferred types
export type Step1Data = z.infer<typeof step1Schema>
export type Step2Data = z.infer<typeof step2Schema>
export type Step3Data = z.infer<typeof step3Schema>
export type Step4Data = z.infer<typeof step4Schema>
export type FullProfileData = z.infer<typeof fullProfileSchema>
