import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

// Mock next/navigation
const mockPush = vi.fn()
vi.mock('next/navigation', () => ({
  useRouter: () => ({
    push: mockPush,
  }),
}))

// Mock useComplianceQuery hook
const mockSubmitQuery = vi.fn()
const mockReset = vi.fn()

vi.mock('@/hooks/use-compliance-query', () => ({
  useComplianceQuery: vi.fn(() => ({
    answer: '',
    citations: [],
    isLoading: false,
    error: null,
    metadata: null,
    submitQuery: mockSubmitQuery,
    reset: mockReset,
  })),
}))

// Mock stripe actions
vi.mock('@/lib/stripe/actions', () => ({
  createPortalSession: vi.fn(() => Promise.resolve({ url: '/pricing' })),
}))

// Mock query components
vi.mock('@/components/query', () => ({
  QueryInput: ({
    value,
    onChange,
    onSubmit,
    isLoading,
    disabled,
  }: {
    value: string
    onChange: (v: string) => void
    onSubmit: (query: string, topic: string) => void
    isLoading: boolean
    disabled: boolean
  }) => (
    <div data-testid="query-input">
      <input
        data-testid="query-textarea"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled || isLoading}
      />
      <button
        data-testid="submit-button"
        onClick={() => onSubmit(value, 'gdpr')}
        disabled={disabled || isLoading || !value}
      >
        {isLoading ? 'Asking...' : 'Ask'}
      </button>
    </div>
  ),
  AnswerStream: ({
    content,
    isLoading,
    error,
    onRetry,
  }: {
    content: string
    isLoading: boolean
    error?: Error | null
    onRetry?: () => void
  }) => (
    <div data-testid="answer-stream">
      {error ? (
        <div data-testid="answer-error">
          {error.message}
          {onRetry && <button onClick={onRetry}>Retry</button>}
        </div>
      ) : (
        <>
          <div data-testid="answer-content">{content}</div>
          {isLoading && <span data-testid="streaming-indicator">Loading...</span>}
        </>
      )}
    </div>
  ),
  CitationList: ({
    citations,
    planLimit,
    onUpgrade,
  }: {
    citations: { source: string; url: string }[]
    planLimit: number
    onUpgrade: () => void
  }) => (
    <div data-testid="citation-list">
      {citations.slice(0, planLimit).map((c, i) => (
        <div key={i} data-testid={`citation-${i}`}>
          {c.source}
        </div>
      ))}
      {citations.length > planLimit && (
        <button onClick={onUpgrade} data-testid="upgrade-button">
          Upgrade
        </button>
      )}
    </div>
  ),
  QueryHistorySidebar: ({
    history,
    onSelectQuery,
    onClearHistory,
  }: {
    history: { id: string; query: string }[]
    onSelectQuery: (q: string) => void
    onClearHistory: () => void
  }) => (
    <div data-testid="query-history">
      {history.map((item) => (
        <button key={item.id} onClick={() => onSelectQuery(item.query)}>
          {item.query}
        </button>
      ))}
      {history.length > 0 && (
        <button onClick={onClearHistory} data-testid="clear-history">
          Clear
        </button>
      )}
    </div>
  ),
}))

// Mock fetch for settings API
const mockFetch = vi.fn()
global.fetch = mockFetch

// Import after mocks
import QueryPage from '@/app/(dashboard)/dashboard/query/page'
import { useComplianceQuery } from '@/hooks/use-compliance-query'

describe('QueryPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset localStorage
    localStorage.clear()
    // Default: successful settings fetch
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      json: () =>
        Promise.resolve({
          profile: {
            company_name: 'Test Co',
            country: 'DE',
            industry: 'technology',
            employee_count: 50,
          },
          subscription: {
            plan: 'free',
            status: 'active',
            current_period_end: null,
          },
        }),
    })
  })

  describe('rendering', () => {
    it('renders the page title and description', async () => {
      render(<QueryPage />)

      expect(screen.getByText('Compliance Q&A')).toBeInTheDocument()
      expect(
        screen.getByText('Ask questions about GDPR and EU AI Act compliance')
      ).toBeInTheDocument()
    })

    it('renders the query input component', async () => {
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('query-input')).toBeInTheDocument()
      })
    })

    it('renders the query history sidebar on desktop', async () => {
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('query-history')).toBeInTheDocument()
      })
    })
  })

  describe('authentication', () => {
    it('redirects to /login when API returns 401', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ error: 'Unauthorized' }),
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(mockPush).toHaveBeenCalledWith('/login')
      })
    })
  })

  describe('plan and quota display', () => {
    it('displays the user plan', async () => {
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('Plan:')).toBeInTheDocument()
        expect(screen.getByText('free')).toBeInTheDocument()
      })
    })

    it('displays the daily query count', async () => {
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('Queries today:')).toBeInTheDocument()
        expect(screen.getByText(/0 \/ 5/)).toBeInTheDocument()
      })
    })

    it('shows premium plan label for premium users', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        status: 200,
        json: () =>
          Promise.resolve({
            profile: { company_name: 'Test Co', country: 'DE' },
            subscription: { plan: 'premium', status: 'active' },
          }),
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('premium')).toBeInTheDocument()
      })
    })
  })

  describe('rate limiting', () => {
    it('shows rate limit warning when daily limit reached', async () => {
      // Set localStorage to indicate limit reached
      localStorage.setItem(
        'kindlast_daily_query_count',
        JSON.stringify({ date: new Date().toDateString(), count: 5 })
      )

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('Daily limit reached')).toBeInTheDocument()
      })
    })

    it('shows upgrade button when rate limited', async () => {
      localStorage.setItem(
        'kindlast_daily_query_count',
        JSON.stringify({ date: new Date().toDateString(), count: 5 })
      )

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('Upgrade to Premium')).toBeInTheDocument()
      })
    })

    it('disables query input when rate limited', async () => {
      localStorage.setItem(
        'kindlast_daily_query_count',
        JSON.stringify({ date: new Date().toDateString(), count: 5 })
      )

      render(<QueryPage />)

      await waitFor(() => {
        const textarea = screen.getByTestId('query-textarea')
        expect(textarea).toBeDisabled()
      })
    })
  })

  describe('query submission', () => {
    it('executes query when submit button is clicked', async () => {
      const user = userEvent.setup()
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('query-input')).toBeInTheDocument()
      })

      const textarea = screen.getByTestId('query-textarea')
      await user.type(textarea, 'What is GDPR?')

      const submitButton = screen.getByTestId('submit-button')
      await user.click(submitButton)

      expect(mockReset).toHaveBeenCalled()
      expect(mockSubmitQuery).toHaveBeenCalledWith('What is GDPR?', 'gdpr')
    })

    it('increments daily query count on submission', async () => {
      const user = userEvent.setup()
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('query-input')).toBeInTheDocument()
      })

      const textarea = screen.getByTestId('query-textarea')
      await user.type(textarea, 'What is GDPR?')

      const submitButton = screen.getByTestId('submit-button')
      await user.click(submitButton)

      const stored = JSON.parse(
        localStorage.getItem('kindlast_daily_query_count') || '{}'
      )
      expect(stored.count).toBe(1)
    })
  })

  describe('streaming response', () => {
    it('shows answer stream when there is content', async () => {
      vi.mocked(useComplianceQuery).mockReturnValue({
        answer: 'This is the answer about GDPR.',
        citations: [],
        isLoading: false,
        error: null,
        metadata: null,
        submitQuery: mockSubmitQuery,
        reset: mockReset,
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('answer-stream')).toBeInTheDocument()
        expect(
          screen.getByText('This is the answer about GDPR.')
        ).toBeInTheDocument()
      })
    })

    it('shows streaming indicator while streaming', async () => {
      vi.mocked(useComplianceQuery).mockReturnValue({
        answer: 'Partial answer...',
        citations: [],
        isLoading: true,
        error: null,
        metadata: null,
        submitQuery: mockSubmitQuery,
        reset: mockReset,
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('streaming-indicator')).toBeInTheDocument()
      })
    })
  })

  describe('citations display', () => {
    it('shows citation list when citations are available', async () => {
      vi.mocked(useComplianceQuery).mockReturnValue({
        answer: 'Answer with citations.',
        citations: [
          { source: 'EUR-Lex', url: 'https://eur-lex.europa.eu/1', title: 'GDPR', excerpt: 'Test', relevance: 0.85 },
          { source: 'EDPB', url: 'https://edpb.europa.eu/1', title: 'Guidelines', excerpt: 'Test', relevance: 0.8 },
        ],
        isLoading: false,
        error: null,
        metadata: { maxRelevance: 0.85, confidenceOk: true, citationCount: 2 },
        submitQuery: mockSubmitQuery,
        reset: mockReset,
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('citation-list')).toBeInTheDocument()
      })
    })
  })

  describe('error handling', () => {
    it('displays error message when query fails', async () => {
      vi.mocked(useComplianceQuery).mockReturnValue({
        answer: '',
        citations: [],
        isLoading: false,
        error: 'Failed to fetch response',
        metadata: null,
        submitQuery: mockSubmitQuery,
        reset: mockReset,
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('answer-error')).toBeInTheDocument()
      })
    })

    it('displays settings error when settings API fails', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: 'Server error' }),
      })

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('Failed to load settings')).toBeInTheDocument()
      })
    })
  })

  describe('query history', () => {
    it('saves query to history on submission', async () => {
      const user = userEvent.setup()
      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByTestId('query-input')).toBeInTheDocument()
      })

      const textarea = screen.getByTestId('query-textarea')
      await user.type(textarea, 'What is GDPR?')

      const submitButton = screen.getByTestId('submit-button')
      await user.click(submitButton)

      const stored = JSON.parse(
        localStorage.getItem('kindlast_query_history') || '[]'
      )
      expect(stored.length).toBe(1)
      expect(stored[0].query).toBe('What is GDPR?')
    })

    it('loads query history from localStorage on mount', async () => {
      localStorage.setItem(
        'kindlast_query_history',
        JSON.stringify([
          { id: '1', query: 'Previous question', timestamp: Date.now() },
        ])
      )

      render(<QueryPage />)

      await waitFor(() => {
        expect(screen.getByText('Previous question')).toBeInTheDocument()
      })
    })
  })

  describe('two-column layout', () => {
    it('renders with grid layout for answer and citations', async () => {
      vi.mocked(useComplianceQuery).mockReturnValue({
        answer: 'Answer content.',
        citations: [
          { source: 'EUR-Lex', url: 'https://eur-lex.europa.eu/1', title: 'GDPR', excerpt: 'Test', relevance: 0.85 },
        ],
        isLoading: false,
        error: null,
        metadata: { maxRelevance: 0.85, confidenceOk: true, citationCount: 1 },
        submitQuery: mockSubmitQuery,
        reset: mockReset,
      })

      const { container } = render(<QueryPage />)

      await waitFor(() => {
        // Verify both answer stream and citation list are rendered
        expect(screen.getByTestId('answer-stream')).toBeInTheDocument()
        expect(screen.getByTestId('citation-list')).toBeInTheDocument()
      })

      // Check that grid layout is applied
      const gridContainer = container.querySelector('.grid')
      expect(gridContainer).toBeInTheDocument()
    })
  })
})
