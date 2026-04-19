import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryInput } from '@/components/query/QueryInput'

describe('QueryInput', () => {
  const defaultProps = {
    value: '',
    onChange: vi.fn(),
    onSubmit: vi.fn(),
    isLoading: false,
    disabled: false,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('rendering', () => {
    it('renders a large textarea for entering compliance questions', () => {
      render(<QueryInput {...defaultProps} />)

      const textarea = screen.getByRole('textbox')
      expect(textarea).toBeInTheDocument()
      expect(textarea).toHaveAttribute(
        'placeholder',
        'Ask a GDPR or EU AI Act compliance question...'
      )
    })

    it('renders with custom placeholder', () => {
      render(<QueryInput {...defaultProps} placeholder="Custom placeholder" />)

      const textarea = screen.getByRole('textbox')
      expect(textarea).toHaveAttribute('placeholder', 'Custom placeholder')
    })

    it('renders topic filter dropdown with all options', () => {
      render(<QueryInput {...defaultProps} />)

      expect(screen.getByText('GDPR')).toBeInTheDocument()
      expect(screen.getByText('AI Act')).toBeInTheDocument()
      expect(screen.getByText('Both')).toBeInTheDocument()
    })

    it('renders example questions section', () => {
      render(<QueryInput {...defaultProps} />)

      expect(
        screen.getByText('What are the lawful bases for processing under GDPR?')
      ).toBeInTheDocument()
      expect(screen.getByText('When is a DPIA required?')).toBeInTheDocument()
      expect(
        screen.getByText('What are the AI Act risk categories?')
      ).toBeInTheDocument()
      expect(
        screen.getByText('How do I respond to a data subject access request?')
      ).toBeInTheDocument()
    })

    it('renders submit button', () => {
      render(<QueryInput {...defaultProps} />)

      const submitButton = screen.getByRole('button', { name: /Ask/i })
      expect(submitButton).toBeInTheDocument()
    })

    it('renders character count indicator', () => {
      render(<QueryInput {...defaultProps} value="Test query" />)

      expect(screen.getByText(/10/)).toBeInTheDocument()
    })

    it('renders clear button when there is text', () => {
      render(<QueryInput {...defaultProps} value="Some text" />)

      const clearButton = screen.getByRole('button', { name: /Clear/i })
      expect(clearButton).toBeInTheDocument()
    })

    it('does not render clear button when textarea is empty', () => {
      render(<QueryInput {...defaultProps} value="" />)

      expect(
        screen.queryByRole('button', { name: /Clear/i })
      ).not.toBeInTheDocument()
    })
  })

  describe('value and onChange', () => {
    it('displays the provided value', () => {
      render(<QueryInput {...defaultProps} value="My compliance question" />)

      const textarea = screen.getByRole('textbox')
      expect(textarea).toHaveValue('My compliance question')
    })

    it('calls onChange when text is entered', async () => {
      const user = userEvent.setup()
      const onChange = vi.fn()
      render(<QueryInput {...defaultProps} onChange={onChange} />)

      const textarea = screen.getByRole('textbox')
      await user.type(textarea, 'a')

      expect(onChange).toHaveBeenCalledWith('a')
    })
  })

  describe('topic filter', () => {
    it('defaults to GDPR topic', () => {
      render(<QueryInput {...defaultProps} />)

      const gdprButton = screen.getByRole('button', { name: 'GDPR' })
      expect(gdprButton).toHaveAttribute('data-selected', 'true')
    })

    it('allows selecting different topics', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(<QueryInput {...defaultProps} value="Test" onSubmit={onSubmit} />)

      const aiActButton = screen.getByRole('button', { name: 'AI Act' })
      await user.click(aiActButton)

      expect(aiActButton).toHaveAttribute('data-selected', 'true')
    })

    it('submits with selected topic', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(<QueryInput {...defaultProps} value="Test question" onSubmit={onSubmit} />)

      // Select AI Act topic
      const aiActButton = screen.getByRole('button', { name: 'AI Act' })
      await user.click(aiActButton)

      // Submit
      const submitButton = screen.getByRole('button', { name: /Ask/i })
      await user.click(submitButton)

      expect(onSubmit).toHaveBeenCalledWith('Test question', 'ai_act')
    })

    it('submits with Both topic', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(<QueryInput {...defaultProps} value="Test question" onSubmit={onSubmit} />)

      // Select Both topic
      const bothButton = screen.getByRole('button', { name: 'Both' })
      await user.click(bothButton)

      // Submit
      const submitButton = screen.getByRole('button', { name: /Ask/i })
      await user.click(submitButton)

      expect(onSubmit).toHaveBeenCalledWith('Test question', 'both')
    })
  })

  describe('example questions', () => {
    it('clicking an example question populates the input', async () => {
      const user = userEvent.setup()
      const onChange = vi.fn()
      render(<QueryInput {...defaultProps} onChange={onChange} />)

      const exampleQuestion = screen.getByText(
        'What are the lawful bases for processing under GDPR?'
      )
      await user.click(exampleQuestion)

      expect(onChange).toHaveBeenCalledWith(
        'What are the lawful bases for processing under GDPR?'
      )
    })

    it('hides example questions when input has text', () => {
      render(<QueryInput {...defaultProps} value="Some query" />)

      expect(
        screen.queryByText('What are the lawful bases for processing under GDPR?')
      ).not.toBeInTheDocument()
    })

    it('hides example questions when loading', () => {
      render(<QueryInput {...defaultProps} isLoading />)

      expect(
        screen.queryByText('What are the lawful bases for processing under GDPR?')
      ).not.toBeInTheDocument()
    })
  })

  describe('submit functionality', () => {
    it('calls onSubmit with query and topic when clicking submit', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(
        <QueryInput {...defaultProps} value="Test compliance question" onSubmit={onSubmit} />
      )

      const submitButton = screen.getByRole('button', { name: /Ask/i })
      await user.click(submitButton)

      expect(onSubmit).toHaveBeenCalledWith('Test compliance question', 'gdpr')
    })

    it('does not submit when textarea is empty', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(<QueryInput {...defaultProps} value="" onSubmit={onSubmit} />)

      const submitButton = screen.getByRole('button', { name: /Ask/i })
      await user.click(submitButton)

      expect(onSubmit).not.toHaveBeenCalled()
    })

    it('does not submit whitespace-only input', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(<QueryInput {...defaultProps} value="   " onSubmit={onSubmit} />)

      const submitButton = screen.getByRole('button', { name: /Ask/i })
      await user.click(submitButton)

      expect(onSubmit).not.toHaveBeenCalled()
    })

    it('trims the query before submitting', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(
        <QueryInput {...defaultProps} value="  Test question  " onSubmit={onSubmit} />
      )

      const submitButton = screen.getByRole('button', { name: /Ask/i })
      await user.click(submitButton)

      expect(onSubmit).toHaveBeenCalledWith('Test question', 'gdpr')
    })
  })

  describe('loading state', () => {
    it('shows loading indicator on submit button when isLoading', () => {
      render(<QueryInput {...defaultProps} value="Test" isLoading />)

      expect(screen.getByText(/Asking/i)).toBeInTheDocument()
    })

    it('disables submit button when isLoading', () => {
      render(<QueryInput {...defaultProps} value="Test" isLoading />)

      const submitButton = screen.getByRole('button', { name: /Asking/i })
      expect(submitButton).toBeDisabled()
    })

    it('disables textarea when isLoading', () => {
      render(<QueryInput {...defaultProps} isLoading />)

      const textarea = screen.getByRole('textbox')
      expect(textarea).toBeDisabled()
    })
  })

  describe('disabled state', () => {
    it('disables textarea when disabled prop is true', () => {
      render(<QueryInput {...defaultProps} disabled />)

      const textarea = screen.getByRole('textbox')
      expect(textarea).toBeDisabled()
    })

    it('disables submit button when disabled', () => {
      render(<QueryInput {...defaultProps} value="Test" disabled />)

      const submitButton = screen.getByRole('button', { name: /Ask/i })
      expect(submitButton).toBeDisabled()
    })

    it('disables topic filter buttons when disabled', () => {
      render(<QueryInput {...defaultProps} disabled />)

      const gdprButton = screen.getByRole('button', { name: 'GDPR' })
      expect(gdprButton).toBeDisabled()
    })
  })

  describe('character count', () => {
    it('displays current character count', () => {
      render(<QueryInput {...defaultProps} value="Hello" />)

      expect(screen.getByText(/5/)).toBeInTheDocument()
    })

    it('shows max character count', () => {
      render(<QueryInput {...defaultProps} value="" />)

      expect(screen.getByText(/2000/)).toBeInTheDocument()
    })

    it('shows warning style when approaching limit', () => {
      const longText = 'a'.repeat(1900)
      render(<QueryInput {...defaultProps} value={longText} />)

      const charCount = screen.getByText(/1900/)
      expect(charCount.closest('[data-near-limit]')).toHaveAttribute(
        'data-near-limit',
        'true'
      )
    })
  })

  describe('keyboard shortcuts', () => {
    it('submits on Cmd+Enter (Mac)', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(
        <QueryInput {...defaultProps} value="Test question" onSubmit={onSubmit} />
      )

      const textarea = screen.getByRole('textbox')
      await user.click(textarea)
      await user.keyboard('{Meta>}{Enter}{/Meta}')

      expect(onSubmit).toHaveBeenCalledWith('Test question', 'gdpr')
    })

    it('submits on Ctrl+Enter (Windows/Linux)', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(
        <QueryInput {...defaultProps} value="Test question" onSubmit={onSubmit} />
      )

      const textarea = screen.getByRole('textbox')
      await user.click(textarea)
      await user.keyboard('{Control>}{Enter}{/Control}')

      expect(onSubmit).toHaveBeenCalledWith('Test question', 'gdpr')
    })

    it('does not submit on plain Enter', async () => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(
        <QueryInput {...defaultProps} value="Test question" onSubmit={onSubmit} />
      )

      const textarea = screen.getByRole('textbox')
      await user.click(textarea)
      await user.keyboard('{Enter}')

      expect(onSubmit).not.toHaveBeenCalled()
    })

    it('does not submit via keyboard when disabled', () => {
      const onSubmit = vi.fn()
      render(
        <QueryInput
          {...defaultProps}
          value="Test question"
          onSubmit={onSubmit}
          disabled
        />
      )

      const textarea = screen.getByRole('textbox')
      fireEvent.keyDown(textarea, { key: 'Enter', metaKey: true })

      expect(onSubmit).not.toHaveBeenCalled()
    })

    it('does not submit via keyboard when loading', () => {
      const onSubmit = vi.fn()
      render(
        <QueryInput
          {...defaultProps}
          value="Test question"
          onSubmit={onSubmit}
          isLoading
        />
      )

      const textarea = screen.getByRole('textbox')
      fireEvent.keyDown(textarea, { key: 'Enter', metaKey: true })

      expect(onSubmit).not.toHaveBeenCalled()
    })
  })

  describe('clear button', () => {
    it('clears the input when clicked', async () => {
      const user = userEvent.setup()
      const onChange = vi.fn()
      render(<QueryInput {...defaultProps} value="Some text" onChange={onChange} />)

      const clearButton = screen.getByRole('button', { name: /Clear/i })
      await user.click(clearButton)

      expect(onChange).toHaveBeenCalledWith('')
    })

    it('is disabled when loading', () => {
      render(<QueryInput {...defaultProps} value="Some text" isLoading />)

      const clearButton = screen.getByRole('button', { name: /Clear/i })
      expect(clearButton).toBeDisabled()
    })
  })

  describe('accessibility', () => {
    it('has accessible label for textarea', () => {
      render(<QueryInput {...defaultProps} />)

      const textarea = screen.getByRole('textbox')
      expect(textarea).toHaveAccessibleName()
    })

    it('shows keyboard shortcut hint', () => {
      render(<QueryInput {...defaultProps} />)

      // Both keyboard shortcuts should be shown
      expect(screen.getByText('Cmd+Enter')).toBeInTheDocument()
      expect(screen.getByText('Ctrl+Enter')).toBeInTheDocument()
    })
  })
})
