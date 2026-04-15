'use client'

import { useState } from 'react'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { WizardProgress } from '@/components/onboarding/wizard-progress'
import { StepCompany, type StepCompanyData } from '@/components/onboarding/step-company'
import { StepData, type StepDataData } from '@/components/onboarding/step-data'
import { StepCompliance, type StepComplianceData } from '@/components/onboarding/step-compliance'
import { StepAiSystems, type StepAiSystemsData } from '@/components/onboarding/step-ai-systems'
import { step1Schema, step2Schema, step3Schema, step4Schema, fullProfileSchema } from '@/lib/schemas/onboarding'
import { saveBusinessProfile, completeOnboarding } from './actions'

const TOTAL_STEPS = 4

export default function OnboardingPage() {
  const [currentStep, setCurrentStep] = useState(0)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [errors, setErrors] = useState<string[]>([])

  const [step1Data, setStep1Data] = useState<StepCompanyData>({
    company_name: '',
    country: 'Estonia',
    industry: '',
    employee_count: undefined,
  })

  const [step2Data, setStep2Data] = useState<StepDataData>({
    processes_personal_data: true,
    data_types: [],
    third_party_processors: [],
    transfers_data_outside_eu: false,
  })

  const [step3Data, setStep3Data] = useState<StepComplianceData>({
    has_privacy_policy: false,
    has_cookie_consent: false,
    has_dpo: false,
    has_breach_notification: false,
    has_dsr_process: false,
  })

  const [step4Data, setStep4Data] = useState<StepAiSystemsData>({
    uses_ai_systems: false,
    ai_system_descriptions: [],
  })

  const validateCurrentStep = (): boolean => {
    setErrors([])
    let result

    switch (currentStep) {
      case 0:
        result = z.safeParse(step1Schema, step1Data)
        break
      case 1:
        result = z.safeParse(step2Schema, step2Data)
        break
      case 2:
        result = z.safeParse(step3Schema, step3Data)
        break
      case 3:
        result = z.safeParse(step4Schema, step4Data)
        break
      default:
        return true
    }

    if (!result?.success) {
      const issues = result?.error?.issues ?? []
      setErrors(issues.map((i: { message: string }) => i.message))
      return false
    }
    return true
  }

  const handleNext = () => {
    if (!validateCurrentStep()) return
    setCurrentStep((prev) => Math.min(prev + 1, TOTAL_STEPS - 1))
  }

  const handleBack = () => {
    setErrors([])
    setCurrentStep((prev) => Math.max(prev - 1, 0))
  }

  const handleSubmit = async () => {
    if (!validateCurrentStep()) return

    const combined = {
      ...step1Data,
      ...step2Data,
      ...step3Data,
      ...step4Data,
    }

    const result = z.safeParse(fullProfileSchema, combined)
    if (!result.success) {
      setErrors(result.error.issues.map((i) => i.message))
      return
    }

    setIsSubmitting(true)
    try {
      await saveBusinessProfile(result.data)
      await completeOnboarding()
    } catch (err) {
      setErrors([err instanceof Error ? err.message : 'An error occurred'])
    } finally {
      setIsSubmitting(false)
    }
  }

  const renderStep = () => {
    switch (currentStep) {
      case 0:
        return <StepCompany data={step1Data} onChange={setStep1Data} />
      case 1:
        return <StepData data={step2Data} onChange={setStep2Data} />
      case 2:
        return <StepCompliance data={step3Data} onChange={setStep3Data} />
      case 3:
        return <StepAiSystems data={step4Data} onChange={setStep4Data} />
      default:
        return null
    }
  }

  const isLastStep = currentStep === TOTAL_STEPS - 1

  return (
    <div className="mx-auto max-w-2xl py-8">
      <Card>
        <CardHeader>
          <CardTitle>Set Up Your Company Profile</CardTitle>
          <div className="pt-4">
            <WizardProgress currentStep={currentStep} totalSteps={TOTAL_STEPS} />
          </div>
        </CardHeader>

        <CardContent>
          {renderStep()}

          {errors.length > 0 && (
            <div className="mt-4 rounded-md bg-destructive/10 p-3">
              {errors.map((error, i) => (
                <p key={i} className="text-sm text-destructive">
                  {error}
                </p>
              ))}
            </div>
          )}
        </CardContent>

        <CardFooter className="flex justify-between">
          <Button
            type="button"
            variant="outline"
            onClick={handleBack}
            disabled={currentStep === 0}
          >
            Back
          </Button>

          {isLastStep ? (
            <Button
              type="button"
              onClick={handleSubmit}
              disabled={isSubmitting}
            >
              {isSubmitting ? 'Saving...' : 'Complete Setup'}
            </Button>
          ) : (
            <Button type="button" onClick={handleNext}>
              Next
            </Button>
          )}
        </CardFooter>
      </Card>
    </div>
  )
}
