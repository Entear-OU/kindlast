import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StepCompany } from '@/components/onboarding/step-company'

describe('StepCompany', () => {
  const defaultProps = {
    data: {
      company_name: '',
      country: 'Estonia',
      industry: '',
      employee_count: undefined as number | undefined,
    },
    onChange: vi.fn(),
  }

  it('renders company name input', () => {
    render(<StepCompany {...defaultProps} />)
    expect(screen.getByLabelText(/company name/i)).toBeInTheDocument()
  })

  it('renders country input', () => {
    render(<StepCompany {...defaultProps} />)
    expect(screen.getByLabelText(/country/i)).toBeInTheDocument()
  })

  it('renders industry input', () => {
    render(<StepCompany {...defaultProps} />)
    expect(screen.getByLabelText(/industry/i)).toBeInTheDocument()
  })

  it('renders employee count input', () => {
    render(<StepCompany {...defaultProps} />)
    expect(screen.getByLabelText(/employee count/i)).toBeInTheDocument()
  })

  it('shows pre-filled data', () => {
    const props = {
      ...defaultProps,
      data: {
        company_name: 'Acme Corp',
        country: 'Germany',
        industry: 'SaaS',
        employee_count: 15,
      },
    }
    render(<StepCompany {...props} />)

    expect(screen.getByLabelText(/company name/i)).toHaveValue('Acme Corp')
    expect(screen.getByLabelText(/country/i)).toHaveValue('Germany')
    expect(screen.getByLabelText(/industry/i)).toHaveValue('SaaS')
    expect(screen.getByLabelText(/employee count/i)).toHaveValue(15)
  })

  it('calls onChange when company name changes', async () => {
    const onChange = vi.fn()
    render(<StepCompany {...defaultProps} onChange={onChange} />)

    const input = screen.getByLabelText(/company name/i)
    await userEvent.type(input, 'Test')

    expect(onChange).toHaveBeenCalled()
  })

  it('marks company name as required', () => {
    render(<StepCompany {...defaultProps} />)
    const input = screen.getByLabelText(/company name/i)
    expect(input).toBeRequired()
  })
})
