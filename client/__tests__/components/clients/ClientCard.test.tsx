import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ClientCard } from '@/components/clients/ClientCard'
import type { Client } from '@/lib/types/database'

const mockClient: Client = {
  id: 'client-1',
  user_id: 'user-1',
  name: 'Acme Corporation',
  description: 'A technology company specializing in SaaS solutions.',
  sector: 'saas',
  country: 'DE',
  employee_count: 50,
  tech_stack: ['Stripe', 'HubSpot', 'AWS', 'Intercom', 'Slack'],
  data_subjects: ['Customers', 'Employees'],
  processing_purposes: ['Customer Relationship Management', 'Payment Processing'],
  status: 'active',
  created_at: '2024-01-15T10:30:00Z',
  updated_at: '2024-01-15T10:30:00Z',
}

describe('ClientCard', () => {
  it('renders the client name', () => {
    render(<ClientCard client={mockClient} />)
    expect(screen.getByText('Acme Corporation')).toBeInTheDocument()
  })

  it('renders the sector label', () => {
    render(<ClientCard client={mockClient} />)
    expect(screen.getByText('SaaS')).toBeInTheDocument()
  })

  it('renders the description', () => {
    render(<ClientCard client={mockClient} />)
    expect(
      screen.getByText(/A technology company specializing in SaaS solutions/)
    ).toBeInTheDocument()
  })

  it('renders the country', () => {
    render(<ClientCard client={mockClient} />)
    expect(screen.getByText('DE')).toBeInTheDocument()
  })

  it('renders the employee count', () => {
    render(<ClientCard client={mockClient} />)
    expect(screen.getByText('50 employees')).toBeInTheDocument()
  })

  it('renders the active status badge', () => {
    render(<ClientCard client={mockClient} />)
    expect(screen.getByText('active')).toBeInTheDocument()
  })

  it('renders tech stack badges (limited to first 4)', () => {
    render(<ClientCard client={mockClient} />)
    expect(screen.getByText('Stripe')).toBeInTheDocument()
    expect(screen.getByText('HubSpot')).toBeInTheDocument()
    expect(screen.getByText('AWS')).toBeInTheDocument()
    expect(screen.getByText('Intercom')).toBeInTheDocument()
    expect(screen.getByText('+1 more')).toBeInTheDocument()
  })

  it('renders archived status when client is archived', () => {
    render(<ClientCard client={{ ...mockClient, status: 'archived' }} />)
    expect(screen.getByText('archived')).toBeInTheDocument()
  })

  it('shows artifact count when provided', () => {
    render(<ClientCard client={mockClient} artifactCount={5} />)
    expect(screen.getByText('5 artifacts generated')).toBeInTheDocument()
  })

  it('shows singular artifact text for count of 1', () => {
    render(<ClientCard client={mockClient} artifactCount={1} />)
    expect(screen.getByText('1 artifact generated')).toBeInTheDocument()
  })

  it('renders without optional fields', () => {
    const minimalClient: Client = {
      ...mockClient,
      description: null,
      sector: null,
      country: null,
      employee_count: null,
      tech_stack: [],
      data_subjects: [],
      processing_purposes: [],
    }
    render(<ClientCard client={minimalClient} />)
    expect(screen.getByText('Acme Corporation')).toBeInTheDocument()
  })

  it('links to the client detail page', () => {
    render(<ClientCard client={mockClient} />)
    const link = screen.getByRole('link')
    expect(link).toHaveAttribute('href', '/dashboard/clients/client-1')
  })
})
