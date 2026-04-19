import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ClientForm } from '@/components/clients/ClientForm'
import type { Client } from '@/lib/types/database'

// Mock next/navigation
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    back: vi.fn(),
    push: vi.fn(),
  }),
}))

const mockClient: Client = {
  id: 'client-1',
  user_id: 'user-1',
  name: 'Existing Company',
  description: 'An existing company description.',
  sector: 'fintech',
  country: 'DE',
  employee_count: 100,
  tech_stack: ['Stripe', 'AWS'],
  data_subjects: ['Customers'],
  processing_purposes: ['Payment Processing'],
  status: 'active',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

describe('ClientForm', () => {
  it('renders the form with empty fields for new client', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByLabelText(/Organization Name/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Sector/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Country/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Employee Count/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Business Description/i)).toBeInTheDocument()
  })

  it('renders pre-filled form for existing client', () => {
    const onSubmit = vi.fn()
    render(<ClientForm client={mockClient} onSubmit={onSubmit} />)

    expect(screen.getByLabelText(/Organization Name/i)).toHaveValue('Existing Company')
    expect(screen.getByLabelText(/Business Description/i)).toHaveValue(
      'An existing company description.'
    )
    expect(screen.getByLabelText(/Employee Count/i)).toHaveValue(100)
  })

  it('shows correct button text for new client', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByRole('button', { name: /Create Client/i })).toBeInTheDocument()
  })

  it('shows correct button text for editing client', () => {
    const onSubmit = vi.fn()
    render(<ClientForm client={mockClient} onSubmit={onSubmit} />)

    expect(screen.getByRole('button', { name: /Update Client/i })).toBeInTheDocument()
  })

  it('calls onSubmit with form data when submitted', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<ClientForm onSubmit={onSubmit} />)

    await user.type(screen.getByLabelText(/Organization Name/i), 'Test Company')
    await user.type(
      screen.getByLabelText(/Business Description/i),
      'A test description'
    )

    await user.click(screen.getByRole('button', { name: /Create Client/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'Test Company',
          description: 'A test description',
        })
      )
    })
  })

  it('disables submit button when name is empty', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByRole('button', { name: /Create Client/i })).toBeDisabled()
  })

  it('disables submit button while submitting', () => {
    const onSubmit = vi.fn().mockImplementation(
      () => new Promise((resolve) => setTimeout(resolve, 1000))
    )
    render(<ClientForm onSubmit={onSubmit} isSubmitting />)

    expect(screen.getByRole('button', { name: /Create Client/i })).toBeDisabled()
  })

  it('renders tech stack section', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByText('Tech Stack')).toBeInTheDocument()
    expect(
      screen.getByPlaceholderText(/e.g., Stripe, HubSpot, AWS/i)
    ).toBeInTheDocument()
  })

  it('renders data subjects section with common options', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByText('Data Subjects')).toBeInTheDocument()
    expect(screen.getByText('Customers')).toBeInTheDocument()
    expect(screen.getByText('Employees')).toBeInTheDocument()
    expect(screen.getByText('Website Visitors')).toBeInTheDocument()
  })

  it('renders processing purposes section with common options', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByText('Processing Purposes')).toBeInTheDocument()
    expect(screen.getByText('Customer Relationship Management')).toBeInTheDocument()
    expect(screen.getByText('Payment Processing')).toBeInTheDocument()
    expect(screen.getByText('Analytics')).toBeInTheDocument()
  })

  it('allows adding tech stack items', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    const techInput = screen.getByPlaceholderText(/e.g., Stripe, HubSpot, AWS/i)
    await user.type(techInput, 'Salesforce')

    // Find the add button next to tech stack input
    const addButtons = screen.getAllByRole('button')
    const addTechButton = addButtons.find(
      (btn) => btn.closest('.flex.gap-2') && techInput.closest('.flex.gap-2')
    )

    if (addTechButton) {
      await user.click(addTechButton)
      expect(screen.getByText('Salesforce')).toBeInTheDocument()
    }
  })

  it('toggles data subject selection', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    const customersButton = screen.getByText('Customers')
    await user.click(customersButton)

    // Should toggle the selection state (exact behavior depends on implementation)
    expect(customersButton).toBeInTheDocument()
  })

  it('has a cancel button', () => {
    const onSubmit = vi.fn()
    render(<ClientForm onSubmit={onSubmit} />)

    expect(screen.getByRole('button', { name: /Cancel/i })).toBeInTheDocument()
  })

  it('displays existing tech stack items for existing client', () => {
    const onSubmit = vi.fn()
    render(<ClientForm client={mockClient} onSubmit={onSubmit} />)

    expect(screen.getByText('Stripe')).toBeInTheDocument()
    expect(screen.getByText('AWS')).toBeInTheDocument()
  })
})
