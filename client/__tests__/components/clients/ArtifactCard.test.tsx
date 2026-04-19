import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ArtifactCard } from '@/components/clients/ArtifactCard'
import type { Artifact } from '@/lib/types/database'

const mockArtifact: Artifact = {
  id: 'artifact-1',
  client_id: 'client-1',
  user_id: 'user-1',
  type: 'ropa',
  status: 'draft',
  title: 'RoPA for Marketing Activities',
  input_context: 'Marketing team processes customer data for email campaigns.',
  generated_content: {},
  edited_content: null,
  citations: [
    {
      index: 1,
      source_url: 'https://example.com/gdpr',
      title: 'GDPR Article 6',
      section: '1(a)',
      chunk_text: 'Consent of the data subject',
    },
    {
      index: 2,
      source_url: 'https://example.com/edpb',
      title: 'EDPB Guidelines',
      section: '2.1',
      chunk_text: 'Processing activities',
    },
  ],
  generation_meta: {
    provider: 'anthropic',
    model: 'claude-3-sonnet',
    tokens_used: 1500,
    latency_ms: 5200,
    corpus_version: '2024-01',
  },
  version: 1,
  created_at: '2024-01-20T14:30:00Z',
  updated_at: '2024-01-20T14:30:00Z',
}

describe('ArtifactCard', () => {
  it('renders the artifact title', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(screen.getByText('RoPA for Marketing Activities')).toBeInTheDocument()
  })

  it('renders the artifact type description', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(screen.getByText('Record of Processing Activities')).toBeInTheDocument()
  })

  it('renders the status badge', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(screen.getByText('Draft')).toBeInTheDocument()
  })

  it('renders the input context preview', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(
      screen.getByText(/Marketing team processes customer data/)
    ).toBeInTheDocument()
  })

  it('renders citation count', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(screen.getByText('2 citations')).toBeInTheDocument()
  })

  it('renders singular citation text for count of 1', () => {
    const artifactWithOneCitation = {
      ...mockArtifact,
      citations: [mockArtifact.citations[0]],
    }
    render(<ArtifactCard artifact={artifactWithOneCitation} clientId="client-1" />)
    expect(screen.getByText('1 citation')).toBeInTheDocument()
  })

  it('renders generation meta information', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(screen.getByText(/Generated with claude-3-sonnet/)).toBeInTheDocument()
    expect(screen.getByText(/5200ms/)).toBeInTheDocument()
  })

  it('renders approved status correctly', () => {
    render(
      <ArtifactCard
        artifact={{ ...mockArtifact, status: 'approved' }}
        clientId="client-1"
      />
    )
    expect(screen.getByText('Approved')).toBeInTheDocument()
  })

  it('shows version badge for version > 1', () => {
    render(
      <ArtifactCard
        artifact={{ ...mockArtifact, version: 3 }}
        clientId="client-1"
      />
    )
    expect(screen.getByText('v3')).toBeInTheDocument()
  })

  it('does not show version badge for version 1', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    expect(screen.queryByText('v1')).not.toBeInTheDocument()
  })

  it('links to the artifact detail page', () => {
    render(<ArtifactCard artifact={mockArtifact} clientId="client-1" />)
    const link = screen.getByRole('link')
    expect(link).toHaveAttribute(
      'href',
      '/dashboard/clients/client-1/artifacts/artifact-1'
    )
  })

  it('renders different artifact types correctly', () => {
    const dpiaArtifact: Artifact = {
      ...mockArtifact,
      type: 'dpia_screening',
      title: null,
    }
    render(<ArtifactCard artifact={dpiaArtifact} clientId="client-1" />)
    expect(screen.getByText('DPIA Screening')).toBeInTheDocument()
    expect(
      screen.getByText('Data Protection Impact Assessment Pre-check')
    ).toBeInTheDocument()
  })

  it('uses type label when title is null', () => {
    const artifactWithoutTitle: Artifact = {
      ...mockArtifact,
      title: null,
    }
    render(<ArtifactCard artifact={artifactWithoutTitle} clientId="client-1" />)
    expect(screen.getByText('RoPA')).toBeInTheDocument()
  })
})
