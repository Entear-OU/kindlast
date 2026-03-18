import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { WizardProgress } from '@/components/onboarding/wizard-progress'

describe('WizardProgress', () => {
  it('renders all step labels', () => {
    render(<WizardProgress currentStep={0} totalSteps={4} />)

    expect(screen.getByText('Company')).toBeInTheDocument()
    expect(screen.getByText('Data')).toBeInTheDocument()
    expect(screen.getByText('Compliance')).toBeInTheDocument()
    expect(screen.getByText('AI Systems')).toBeInTheDocument()
  })

  it('renders correct number of steps', () => {
    render(<WizardProgress currentStep={0} totalSteps={4} />)

    const stepIndicators = screen.getAllByRole('listitem')
    expect(stepIndicators).toHaveLength(4)
  })

  it('highlights the current step', () => {
    render(<WizardProgress currentStep={1} totalSteps={4} />)

    const steps = screen.getAllByRole('listitem')
    expect(steps[1]).toHaveAttribute('aria-current', 'step')
  })

  it('marks completed steps', () => {
    render(<WizardProgress currentStep={2} totalSteps={4} />)

    const steps = screen.getAllByRole('listitem')
    // Steps 0 and 1 should be completed
    expect(steps[0]).toHaveAttribute('data-completed', 'true')
    expect(steps[1]).toHaveAttribute('data-completed', 'true')
    // Current step not completed
    expect(steps[2]).not.toHaveAttribute('data-completed', 'true')
    // Future step not completed
    expect(steps[3]).not.toHaveAttribute('data-completed', 'true')
  })
})
