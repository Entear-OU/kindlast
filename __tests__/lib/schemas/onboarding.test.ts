import { describe, it, expect } from 'vitest'
import { z } from 'zod'
import {
  step1Schema,
  step2Schema,
  step3Schema,
  step4Schema,
  fullProfileSchema,
} from '@/lib/schemas/onboarding'

describe('lib/schemas/onboarding', () => {
  describe('step1Schema (Company Basics)', () => {
    it('accepts valid data', () => {
      const data = {
        company_name: 'Acme Corp',
        country: 'Estonia',
        industry: 'Technology',
        employee_count: 25,
      }
      const result = z.safeParse(step1Schema, data)
      expect(result.success).toBe(true)
      if (result.success) {
        expect(result.data.company_name).toBe('Acme Corp')
        expect(result.data.country).toBe('Estonia')
      }
    })

    it('requires company_name', () => {
      const data = { country: 'Estonia' }
      const result = z.safeParse(step1Schema, data)
      expect(result.success).toBe(false)
    })

    it('rejects empty company_name', () => {
      const data = { company_name: '', country: 'Estonia' }
      const result = z.safeParse(step1Schema, data)
      expect(result.success).toBe(false)
    })

    it('defaults country to Estonia', () => {
      const data = { company_name: 'Acme Corp' }
      const result = z.safeParse(step1Schema, data)
      expect(result.success).toBe(true)
      if (result.success) {
        expect(result.data.country).toBe('Estonia')
      }
    })

    it('allows optional industry and employee_count', () => {
      const data = { company_name: 'Acme Corp' }
      const result = z.safeParse(step1Schema, data)
      expect(result.success).toBe(true)
      if (result.success) {
        expect(result.data.industry).toBeUndefined()
        expect(result.data.employee_count).toBeUndefined()
      }
    })
  })

  describe('step2Schema (Data Processing)', () => {
    it('accepts valid data', () => {
      const data = {
        processes_personal_data: true,
        data_types: ['email', 'payment'],
        third_party_processors: ['Stripe'],
        transfers_data_outside_eu: false,
      }
      const result = z.safeParse(step2Schema, data)
      expect(result.success).toBe(true)
    })

    it('rejects missing required boolean fields', () => {
      const data = {
        data_types: ['email'],
        third_party_processors: [],
      }
      const result = z.safeParse(step2Schema, data)
      expect(result.success).toBe(false)
    })

    it('accepts empty arrays for data_types and third_party_processors', () => {
      const data = {
        processes_personal_data: false,
        data_types: [],
        third_party_processors: [],
        transfers_data_outside_eu: false,
      }
      const result = z.safeParse(step2Schema, data)
      expect(result.success).toBe(true)
    })
  })

  describe('step3Schema (Compliance Measures)', () => {
    it('accepts valid data with all booleans', () => {
      const data = {
        has_privacy_policy: true,
        has_cookie_consent: true,
        has_dpo: false,
        has_breach_notification: false,
        has_dsr_process: false,
      }
      const result = z.safeParse(step3Schema, data)
      expect(result.success).toBe(true)
    })

    it('rejects missing fields', () => {
      const data = {
        has_privacy_policy: true,
      }
      const result = z.safeParse(step3Schema, data)
      expect(result.success).toBe(false)
    })
  })

  describe('step4Schema (AI Systems)', () => {
    it('accepts data with no AI systems', () => {
      const data = {
        uses_ai_systems: false,
      }
      const result = z.safeParse(step4Schema, data)
      expect(result.success).toBe(true)
    })

    it('accepts data with AI systems described', () => {
      const data = {
        uses_ai_systems: true,
        ai_system_descriptions: [
          {
            name: 'Chatbot',
            purpose: 'Customer support',
            dataUsed: 'Customer messages',
            isAutomatedDecision: false,
          },
        ],
      }
      const result = z.safeParse(step4Schema, data)
      expect(result.success).toBe(true)
    })

    it('requires ai_system_descriptions when uses_ai_systems is true', () => {
      const data = {
        uses_ai_systems: true,
      }
      const result = z.safeParse(step4Schema, data)
      expect(result.success).toBe(false)
    })

    it('requires ai_system_descriptions to be non-empty when uses_ai_systems is true', () => {
      const data = {
        uses_ai_systems: true,
        ai_system_descriptions: [],
      }
      const result = z.safeParse(step4Schema, data)
      expect(result.success).toBe(false)
    })
  })

  describe('fullProfileSchema (Combined)', () => {
    it('accepts valid combined data from all steps', () => {
      const data = {
        // Step 1
        company_name: 'Acme Corp',
        country: 'Estonia',
        industry: 'Technology',
        employee_count: 25,
        // Step 2
        processes_personal_data: true,
        data_types: ['email', 'payment'],
        third_party_processors: ['Stripe'],
        transfers_data_outside_eu: false,
        // Step 3
        has_privacy_policy: true,
        has_cookie_consent: true,
        has_dpo: false,
        has_breach_notification: false,
        has_dsr_process: false,
        // Step 4
        uses_ai_systems: false,
      }
      const result = z.safeParse(fullProfileSchema, data)
      expect(result.success).toBe(true)
    })

    it('rejects data missing required fields from any step', () => {
      const data = {
        // Missing company_name from Step 1
        country: 'Estonia',
        processes_personal_data: true,
        data_types: [],
        third_party_processors: [],
        transfers_data_outside_eu: false,
        has_privacy_policy: true,
        has_cookie_consent: true,
        has_dpo: false,
        has_breach_notification: false,
        has_dsr_process: false,
        uses_ai_systems: false,
      }
      const result = z.safeParse(fullProfileSchema, data)
      expect(result.success).toBe(false)
    })

    it('validates AI systems in combined schema', () => {
      const data = {
        company_name: 'Acme Corp',
        country: 'Estonia',
        processes_personal_data: true,
        data_types: ['email'],
        third_party_processors: [],
        transfers_data_outside_eu: false,
        has_privacy_policy: true,
        has_cookie_consent: true,
        has_dpo: false,
        has_breach_notification: false,
        has_dsr_process: false,
        uses_ai_systems: true,
        ai_system_descriptions: [
          {
            name: 'AI Assistant',
            purpose: 'Internal use',
            dataUsed: 'Employee data',
            isAutomatedDecision: true,
          },
        ],
      }
      const result = z.safeParse(fullProfileSchema, data)
      expect(result.success).toBe(true)
    })
  })
})
