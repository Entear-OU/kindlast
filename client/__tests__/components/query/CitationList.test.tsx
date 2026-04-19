import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CitationList } from '@/components/query/CitationList'
import type { Citation } from '@/lib/api/types'

// Save original location
const originalLocation = window.location

// Mock citations for testing
const mockCitations: Citation[] = [
  {
    source: 'EUR-Lex',
    title: 'General Data Protection Regulation',
    url: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj',
    excerpt: 'Article 6 - Lawfulness of processing. Processing shall be lawful only if and to the extent that at least one of the following applies...',
    relevance: 0.95,
  },
  {
    source: 'EDPB',
    title: 'Guidelines on Consent',
    url: 'https://edpb.europa.eu/guidelines/consent',
    excerpt: 'Consent should be given by a clear affirmative act establishing a freely given, specific, informed and unambiguous indication of the data subject\'s agreement.',
    relevance: 0.88,
  },
  {
    source: 'ICO',
    title: 'Lawful Basis for Processing',
    url: 'https://ico.org.uk/guidance/lawful-basis',
    excerpt: 'You must have a valid lawful basis in order to process personal data. There are six available lawful bases for processing.',
    relevance: 0.82,
  },
  {
    source: 'CNIL',
    title: 'Guide on Data Protection',
    url: 'https://cnil.fr/guide/gdpr',
    excerpt: 'The GDPR provides a framework for data protection across the European Union. This guide explains the key principles and requirements.',
    relevance: 0.75,
  },
  {
    source: 'DPA',
    title: 'National Implementation Guidelines',
    url: 'https://dpa.example.com/guidelines',
    excerpt: 'National data protection authorities provide guidance on implementing GDPR requirements in specific contexts and jurisdictions.',
    relevance: 0.70,
  },
]

beforeEach(() => {
  // Reset location before each test
  Object.defineProperty(window, 'location', {
    value: { ...originalLocation, hash: '' },
    writable: true,
  })
})

afterEach(() => {
  // Restore original location
  Object.defineProperty(window, 'location', {
    value: originalLocation,
    writable: true,
  })
})

describe('CitationList', () => {
  const mockOnUpgrade = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('basic rendering', () => {
    it('renders the citations header with count', () => {
      render(<CitationList citations={mockCitations} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText(/Sources \(5\)/)).toBeInTheDocument()
    })

    it('renders all citations when planLimit is high enough', () => {
      render(<CitationList citations={mockCitations} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText('General Data Protection Regulation')).toBeInTheDocument()
      expect(screen.getByText('Guidelines on Consent')).toBeInTheDocument()
      expect(screen.getByText('Lawful Basis for Processing')).toBeInTheDocument()
      expect(screen.getByText('Guide on Data Protection')).toBeInTheDocument()
      expect(screen.getByText('National Implementation Guidelines')).toBeInTheDocument()
    })

    it('renders correct id anchors for each citation', () => {
      render(<CitationList citations={mockCitations} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(document.getElementById('citation-1')).toBeInTheDocument()
      expect(document.getElementById('citation-2')).toBeInTheDocument()
      expect(document.getElementById('citation-3')).toBeInTheDocument()
    })

    it('displays citation numbers correctly', () => {
      render(<CitationList citations={mockCitations.slice(0, 2)} planLimit={10} onUpgrade={mockOnUpgrade} />)
      const numberBadges = screen.getAllByTestId(/citation-number/)
      expect(numberBadges[0]).toHaveTextContent('1')
      expect(numberBadges[1]).toHaveTextContent('2')
    })
  })

  describe('citation content', () => {
    it('renders source title', () => {
      render(<CitationList citations={[mockCitations[0]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText('General Data Protection Regulation')).toBeInTheDocument()
    })

    it('renders source URL as clickable link', () => {
      render(<CitationList citations={[mockCitations[0]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      const link = screen.getByRole('link', { name: /eur-lex/i })
      expect(link).toHaveAttribute('href', 'https://eur-lex.europa.eu/eli/reg/2016/679/oj')
      expect(link).toHaveAttribute('target', '_blank')
      expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
    })

    it('renders excerpt text', () => {
      render(<CitationList citations={[mockCitations[0]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText(/Article 6 - Lawfulness of processing/)).toBeInTheDocument()
    })
  })

  describe('excerpt expansion', () => {
    const longExcerpt = 'A'.repeat(300) + ' This is the end of a very long excerpt that should be truncated.'
    const citationWithLongExcerpt: Citation = {
      source: 'Test',
      title: 'Test Document',
      url: 'https://example.com',
      excerpt: longExcerpt,
      relevance: 0.9,
    }

    it('truncates long excerpts by default', () => {
      render(<CitationList citations={[citationWithLongExcerpt]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText(/show more/i)).toBeInTheDocument()
    })

    it('expands excerpt when show more is clicked', () => {
      render(<CitationList citations={[citationWithLongExcerpt]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      const showMoreButton = screen.getByText(/show more/i)
      fireEvent.click(showMoreButton)
      expect(screen.getByText(/show less/i)).toBeInTheDocument()
      expect(screen.getByText(/This is the end of a very long excerpt/)).toBeInTheDocument()
    })

    it('collapses excerpt when show less is clicked', () => {
      render(<CitationList citations={[citationWithLongExcerpt]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      const showMoreButton = screen.getByText(/show more/i)
      fireEvent.click(showMoreButton)
      const showLessButton = screen.getByText(/show less/i)
      fireEvent.click(showLessButton)
      expect(screen.getByText(/show more/i)).toBeInTheDocument()
    })
  })

  describe('tier badges', () => {
    it('shows Tier 1 badge for EUR-Lex sources', () => {
      render(<CitationList citations={[mockCitations[0]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText('Tier 1')).toBeInTheDocument()
    })

    it('shows Tier 2 badge for EDPB sources', () => {
      render(<CitationList citations={[mockCitations[1]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText('Tier 2')).toBeInTheDocument()
    })

    it('shows Tier 3 badge for national DPA sources', () => {
      render(<CitationList citations={[mockCitations[2]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText('Tier 3')).toBeInTheDocument()
    })
  })

  describe('freemium limiting', () => {
    it('limits visible citations when planLimit is set lower than total', () => {
      render(<CitationList citations={mockCitations} planLimit={3} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText('General Data Protection Regulation')).toBeInTheDocument()
      expect(screen.getByText('Guidelines on Consent')).toBeInTheDocument()
      expect(screen.getByText('Lawful Basis for Processing')).toBeInTheDocument()
      expect(screen.queryByText('Guide on Data Protection')).not.toBeInTheDocument()
      expect(screen.queryByText('National Implementation Guidelines')).not.toBeInTheDocument()
    })

    it('shows upgrade prompt when there are hidden citations', () => {
      render(<CitationList citations={mockCitations} planLimit={3} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByText(/more source/i)).toBeInTheDocument()
    })

    it('does not show upgrade prompt when all citations are visible', () => {
      render(<CitationList citations={mockCitations.slice(0, 3)} planLimit={3} onUpgrade={mockOnUpgrade} />)
      expect(screen.queryByTestId('freemium-gate')).not.toBeInTheDocument()
    })

    it('calls onUpgrade when upgrade button is clicked', () => {
      render(<CitationList citations={mockCitations} planLimit={3} onUpgrade={mockOnUpgrade} />)
      const upgradeButton = screen.getByRole('button', { name: /upgrade/i })
      fireEvent.click(upgradeButton)
      expect(mockOnUpgrade).toHaveBeenCalledTimes(1)
    })
  })

  describe('focused citation highlighting', () => {
    it('highlights citation when URL hash matches', () => {
      // Simulate hash in URL
      Object.defineProperty(window, 'location', {
        value: { ...window.location, hash: '#citation-1' },
        writable: true,
      })

      render(<CitationList citations={mockCitations} planLimit={10} onUpgrade={mockOnUpgrade} />)
      const citation = document.getElementById('citation-1')
      expect(citation).toHaveClass('ring-2')
    })
  })

  describe('empty state', () => {
    it('returns null when no citations', () => {
      const { container } = render(<CitationList citations={[]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(container.firstChild).toBeNull()
    })
  })

  describe('accessibility', () => {
    it('has proper heading structure', () => {
      render(<CitationList citations={mockCitations} planLimit={10} onUpgrade={mockOnUpgrade} />)
      expect(screen.getByRole('heading', { level: 3 })).toHaveTextContent(/sources/i)
    })

    it('links open in new tab with proper rel attribute', () => {
      render(<CitationList citations={[mockCitations[0]]} planLimit={10} onUpgrade={mockOnUpgrade} />)
      const link = screen.getByRole('link', { name: /eur-lex/i })
      expect(link).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
    })
  })
})
