import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  QueryHistorySidebar,
  type QueryHistoryItem,
} from '@/components/query/QueryHistorySidebar'

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  History: (props: Record<string, unknown>) => (
    <svg data-testid="icon-history" {...props} />
  ),
  X: (props: Record<string, unknown>) => <svg data-testid="icon-x" {...props} />,
}))

describe('QueryHistorySidebar', () => {
  const mockOnSelectQuery = vi.fn()
  const mockOnClearHistory = vi.fn()

  const mockHistory: QueryHistoryItem[] = [
    {
      id: '1',
      query: 'What is GDPR?',
      timestamp: Date.now() - 60000,
    },
    {
      id: '2',
      query: 'Do I need a DPO?',
      timestamp: Date.now() - 120000,
    },
    {
      id: '3',
      query: 'What are the lawful bases for processing?',
      timestamp: Date.now() - 180000,
    },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows empty state when history is empty', () => {
    render(
      <QueryHistorySidebar
        history={[]}
        onSelectQuery={mockOnSelectQuery}
        onClearHistory={mockOnClearHistory}
      />
    )

    expect(screen.getByText('No recent queries')).toBeInTheDocument()
    expect(
      screen.getByText('Your recent questions will appear here')
    ).toBeInTheDocument()
  })

  it('renders history items', () => {
    render(
      <QueryHistorySidebar
        history={mockHistory}
        onSelectQuery={mockOnSelectQuery}
        onClearHistory={mockOnClearHistory}
      />
    )

    expect(screen.getByText('What is GDPR?')).toBeInTheDocument()
    expect(screen.getByText('Do I need a DPO?')).toBeInTheDocument()
    expect(
      screen.getByText('What are the lawful bases for processing?')
    ).toBeInTheDocument()
  })

  it('displays header with clear button when history exists', () => {
    render(
      <QueryHistorySidebar
        history={mockHistory}
        onSelectQuery={mockOnSelectQuery}
        onClearHistory={mockOnClearHistory}
      />
    )

    expect(screen.getByText('Recent Queries')).toBeInTheDocument()
    expect(screen.getByLabelText('Clear history')).toBeInTheDocument()
  })

  it('calls onSelectQuery when a history item is clicked', async () => {
    const user = userEvent.setup()
    render(
      <QueryHistorySidebar
        history={mockHistory}
        onSelectQuery={mockOnSelectQuery}
        onClearHistory={mockOnClearHistory}
      />
    )

    await user.click(screen.getByText('What is GDPR?'))
    expect(mockOnSelectQuery).toHaveBeenCalledWith('What is GDPR?')
  })

  it('calls onClearHistory when clear button is clicked', async () => {
    const user = userEvent.setup()
    render(
      <QueryHistorySidebar
        history={mockHistory}
        onSelectQuery={mockOnSelectQuery}
        onClearHistory={mockOnClearHistory}
      />
    )

    await user.click(screen.getByLabelText('Clear history'))
    expect(mockOnClearHistory).toHaveBeenCalled()
  })

  it('has correct test id', () => {
    render(
      <QueryHistorySidebar
        history={mockHistory}
        onSelectQuery={mockOnSelectQuery}
        onClearHistory={mockOnClearHistory}
      />
    )

    expect(screen.getByTestId('query-history-sidebar')).toBeInTheDocument()
  })
})
