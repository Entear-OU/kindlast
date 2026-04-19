import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { FreemiumGate } from '@/components/query/FreemiumGate'

describe('FreemiumGate', () => {
  const defaultProps = {
    hiddenCount: 7,
    onUpgrade: vi.fn(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('content display', () => {
    it('displays singular "source" when only 1 hidden', () => {
      render(<FreemiumGate hiddenCount={1} onUpgrade={vi.fn()} />)
      expect(screen.getByText(/1 more source available/i)).toBeInTheDocument()
    })

    it('displays plural "sources" when multiple hidden', () => {
      render(<FreemiumGate {...defaultProps} />)
      expect(screen.getByText(/7 more sources available/i)).toBeInTheDocument()
    })

    it('displays premium features list', () => {
      render(<FreemiumGate {...defaultProps} />)
      expect(screen.getByText(/full citations/i)).toBeInTheDocument()
      expect(screen.getByText(/eu ai act/i)).toBeInTheDocument()
      expect(screen.getByText(/document generation/i)).toBeInTheDocument()
    })
  })

  describe('upgrade button', () => {
    it('renders an upgrade button', () => {
      render(<FreemiumGate {...defaultProps} />)
      expect(screen.getByRole('button', { name: /upgrade/i })).toBeInTheDocument()
    })

    it('calls onUpgrade when upgrade button is clicked', () => {
      const onUpgrade = vi.fn()
      render(<FreemiumGate hiddenCount={5} onUpgrade={onUpgrade} />)

      fireEvent.click(screen.getByRole('button', { name: /upgrade/i }))
      expect(onUpgrade).toHaveBeenCalledTimes(1)
    })
  })

  describe('dismiss functionality', () => {
    it('renders dismiss button when onDismiss is provided', () => {
      const onDismiss = vi.fn()
      render(<FreemiumGate {...defaultProps} onDismiss={onDismiss} />)

      expect(screen.getByRole('button', { name: /dismiss/i })).toBeInTheDocument()
    })

    it('does not render dismiss button when onDismiss is not provided', () => {
      render(<FreemiumGate {...defaultProps} />)

      expect(screen.queryByRole('button', { name: /dismiss/i })).not.toBeInTheDocument()
    })

    it('calls onDismiss when dismiss button is clicked', () => {
      const onDismiss = vi.fn()
      render(<FreemiumGate {...defaultProps} onDismiss={onDismiss} />)

      fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))
      expect(onDismiss).toHaveBeenCalledTimes(1)
    })
  })

  describe('premium pricing', () => {
    it('displays premium pricing information', () => {
      render(<FreemiumGate {...defaultProps} />)
      expect(screen.getByText(/49/)).toBeInTheDocument()
    })
  })

  describe('styling', () => {
    it('has a dashed border styling', () => {
      const { container } = render(<FreemiumGate {...defaultProps} />)
      const gate = container.firstChild as HTMLElement
      expect(gate).toHaveClass('border-dashed')
    })

    it('is visually distinct as an upgrade prompt', () => {
      const { container } = render(<FreemiumGate {...defaultProps} />)
      const gate = container.firstChild as HTMLElement
      expect(gate).toHaveClass('rounded-lg')
    })
  })

  describe('edge cases', () => {
    it('handles large numbers of hidden citations', () => {
      render(<FreemiumGate hiddenCount={97} onUpgrade={vi.fn()} />)
      expect(screen.getByText(/97 more sources/i)).toBeInTheDocument()
    })
  })

  describe('accessibility', () => {
    it('upgrade button is accessible', () => {
      render(<FreemiumGate {...defaultProps} />)
      const button = screen.getByRole('button', { name: /upgrade/i })
      expect(button).toBeInTheDocument()
    })

    it('dismiss button has appropriate aria label when provided', () => {
      render(<FreemiumGate {...defaultProps} onDismiss={vi.fn()} />)
      const button = screen.getByRole('button', { name: /dismiss/i })
      expect(button).toHaveAttribute('aria-label')
    })
  })
})
