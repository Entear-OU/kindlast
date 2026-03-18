import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { LegalDisclaimer } from '@/components/dashboard/legal-disclaimer'

describe('LegalDisclaimer', () => {
  it('renders the mandatory legal disclaimer text', () => {
    render(<LegalDisclaimer />)

    expect(
      screen.getByText(/Kindlast provides AI-generated compliance guidance for educational and planning purposes/i)
    ).toBeInTheDocument()
  })

  it('mentions it is not a substitute for legal advice', () => {
    render(<LegalDisclaimer />)

    expect(
      screen.getByText(/not a substitute for professional legal advice/i)
    ).toBeInTheDocument()
  })

  it('mentions consulting a qualified professional', () => {
    render(<LegalDisclaimer />)

    expect(
      screen.getByText(/consult a qualified data protection attorney or certified DPO/i)
    ).toBeInTheDocument()
  })
})
