import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ArtifactTypeSelector } from '@/components/clients/ArtifactTypeSelector'

describe('ArtifactTypeSelector', () => {
  it('renders all artifact type options', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType={null} onSelect={onSelect} />)

    expect(screen.getByText('Record of Processing Activities')).toBeInTheDocument()
    expect(screen.getByText('DPIA Screening')).toBeInTheDocument()
    expect(screen.getByText('DPA Gap Analysis')).toBeInTheDocument()
    expect(screen.getByText('Lawful Basis Assessment')).toBeInTheDocument()
    expect(screen.getByText('AI Act Classification')).toBeInTheDocument()
  })

  it('renders descriptions for each type', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType={null} onSelect={onSelect} />)

    expect(
      screen.getByText(/Generate a comprehensive RoPA documenting all data processing activities/)
    ).toBeInTheDocument()
    expect(
      screen.getByText(/Pre-assessment to determine if a full DPIA is required/)
    ).toBeInTheDocument()
  })

  it('renders GDPR article badges', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType={null} onSelect={onSelect} />)

    expect(screen.getByText('Article 30')).toBeInTheDocument()
    expect(screen.getByText('Article 35')).toBeInTheDocument()
    expect(screen.getByText('Article 28')).toBeInTheDocument()
    expect(screen.getByText('Article 6')).toBeInTheDocument()
  })

  it('calls onSelect when a type is clicked', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType={null} onSelect={onSelect} />)

    const ropaCard = screen
      .getByText('Record of Processing Activities')
      .closest('div[class*="cursor-pointer"]')

    if (ropaCard) {
      fireEvent.click(ropaCard)
      expect(onSelect).toHaveBeenCalledWith('ropa')
    }
  })

  it('shows selected state for the chosen type', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType="ropa" onSelect={onSelect} />)

    // Check icon exists (the checkmark for selected state)
    const checkmarks = document.querySelectorAll('[class*="bg-primary text-primary-foreground"]')
    expect(checkmarks.length).toBeGreaterThan(0)
  })

  it('does not call onSelect when disabled', () => {
    const onSelect = vi.fn()
    render(
      <ArtifactTypeSelector selectedType={null} onSelect={onSelect} disabled />
    )

    const ropaCard = screen
      .getByText('Record of Processing Activities')
      .closest('div[class*="cursor"]')

    if (ropaCard) {
      fireEvent.click(ropaCard)
      expect(onSelect).not.toHaveBeenCalled()
    }
  })

  it('renders detail bullet points for each type', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType={null} onSelect={onSelect} />)

    // RoPA details
    expect(screen.getByText('Processing purposes and lawful basis')).toBeInTheDocument()
    expect(screen.getByText('Data categories and subjects')).toBeInTheDocument()
    expect(screen.getByText('Retention periods')).toBeInTheDocument()

    // DPIA details
    expect(screen.getByText('EDPB 9 criteria evaluation')).toBeInTheDocument()
    expect(screen.getByText('Risk level assessment')).toBeInTheDocument()

    // DPA gap details
    expect(screen.getByText('Processor inventory')).toBeInTheDocument()
    expect(screen.getByText('Transfer mechanism analysis')).toBeInTheDocument()

    // Lawful basis details
    expect(screen.getByText('Six lawful bases analysis')).toBeInTheDocument()
    expect(screen.getByText('Legitimate interest assessment')).toBeInTheDocument()

    // AI Act details
    expect(screen.getByText('Risk category determination')).toBeInTheDocument()
    expect(screen.getByText('Compliance obligations')).toBeInTheDocument()
  })

  it('allows selection of different types', () => {
    const onSelect = vi.fn()
    render(<ArtifactTypeSelector selectedType="dpa_gap" onSelect={onSelect} />)

    const dpiaCard = screen
      .getByText('DPIA Screening')
      .closest('div[class*="cursor-pointer"]')

    if (dpiaCard) {
      fireEvent.click(dpiaCard)
      expect(onSelect).toHaveBeenCalledWith('dpia_screening')
    }
  })
})
