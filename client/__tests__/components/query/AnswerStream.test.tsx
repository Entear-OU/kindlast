import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AnswerStream } from '@/components/query/AnswerStream'

describe('AnswerStream', () => {
  describe('content rendering', () => {
    it('renders markdown content correctly', () => {
      render(
        <AnswerStream
          content="This is **bold** and *italic* text."
          isStreaming={false}
        />
      )
      expect(screen.getByText('bold')).toBeInTheDocument()
      expect(screen.getByText('italic')).toBeInTheDocument()
    })

    it('renders code blocks in markdown', () => {
      render(
        <AnswerStream
          content="Here is some `inline code` text."
          isStreaming={false}
        />
      )
      expect(screen.getByText('inline code')).toBeInTheDocument()
    })

    it('renders lists in markdown', () => {
      const listContent = `- Item one
- Item two
- Item three`
      render(
        <AnswerStream
          content={listContent}
          isStreaming={false}
        />
      )
      expect(screen.getByText('Item one')).toBeInTheDocument()
      expect(screen.getByText('Item two')).toBeInTheDocument()
      expect(screen.getByText('Item three')).toBeInTheDocument()
    })

    it('renders headings in markdown', () => {
      render(
        <AnswerStream
          content="# Main Heading\n\nSome text content."
          isStreaming={false}
        />
      )
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
        'Main Heading'
      )
    })

    it('renders empty state when content is empty', () => {
      const { container } = render(
        <AnswerStream content="" isStreaming={false} />
      )
      expect(container.querySelector('[data-testid="answer-content"]')).toBeInTheDocument()
    })
  })

  describe('streaming cursor', () => {
    it('shows blinking cursor when streaming is active', () => {
      const { container } = render(
        <AnswerStream content="Some content" isStreaming={true} />
      )
      const cursor = container.querySelector('[data-testid="streaming-cursor"]')
      expect(cursor).toBeInTheDocument()
    })

    it('hides cursor when streaming is complete', () => {
      const { container } = render(
        <AnswerStream content="Final content" isStreaming={false} />
      )
      const cursor = container.querySelector('[data-testid="streaming-cursor"]')
      expect(cursor).not.toBeInTheDocument()
    })
  })

  describe('citation superscripts', () => {
    it('renders citation markers as clickable superscripts', () => {
      render(
        <AnswerStream
          content="This is a claim [1] with a citation."
          isStreaming={false}
        />
      )
      const citation = screen.getByRole('link', { name: '[1]' })
      expect(citation).toBeInTheDocument()
      expect(citation).toHaveAttribute('href', '#citation-1')
    })

    it('renders multiple citation markers correctly', () => {
      render(
        <AnswerStream
          content="First claim [1] and second claim [2] with citations."
          isStreaming={false}
        />
      )
      const citation1 = screen.getByRole('link', { name: '[1]' })
      const citation2 = screen.getByRole('link', { name: '[2]' })
      expect(citation1).toHaveAttribute('href', '#citation-1')
      expect(citation2).toHaveAttribute('href', '#citation-2')
    })

    it('renders citation markers as superscript elements', () => {
      const { container } = render(
        <AnswerStream
          content="Citation here [3] in text."
          isStreaming={false}
        />
      )
      const superscript = container.querySelector('sup')
      expect(superscript).toBeInTheDocument()
    })

    it('handles double-digit citations correctly', () => {
      render(
        <AnswerStream
          content="Many citations [10] and [12] in the text."
          isStreaming={false}
        />
      )
      const citation10 = screen.getByRole('link', { name: '[10]' })
      const citation12 = screen.getByRole('link', { name: '[12]' })
      expect(citation10).toHaveAttribute('href', '#citation-10')
      expect(citation12).toHaveAttribute('href', '#citation-12')
    })
  })

  describe('low confidence warning', () => {
    it('shows warning banner when confidence is below 0.72', () => {
      render(
        <AnswerStream
          content="Some answer content"
          isStreaming={false}
          confidence={0.65}
        />
      )
      expect(
        screen.getByText(/limited source material found/i)
      ).toBeInTheDocument()
    })

    it('shows warning banner at confidence exactly 0.71', () => {
      render(
        <AnswerStream
          content="Some answer content"
          isStreaming={false}
          confidence={0.71}
        />
      )
      expect(
        screen.getByText(/limited source material found/i)
      ).toBeInTheDocument()
    })

    it('does not show warning when confidence is 0.72 or higher', () => {
      render(
        <AnswerStream
          content="Some answer content"
          isStreaming={false}
          confidence={0.72}
        />
      )
      expect(
        screen.queryByText(/limited source material found/i)
      ).not.toBeInTheDocument()
    })

    it('does not show warning when confidence is high', () => {
      render(
        <AnswerStream
          content="Some answer content"
          isStreaming={false}
          confidence={0.95}
        />
      )
      expect(
        screen.queryByText(/limited source material found/i)
      ).not.toBeInTheDocument()
    })

    it('does not show warning when confidence is undefined', () => {
      render(
        <AnswerStream
          content="Some answer content"
          isStreaming={false}
        />
      )
      expect(
        screen.queryByText(/limited source material found/i)
      ).not.toBeInTheDocument()
    })
  })

  describe('loading skeleton', () => {
    it('shows loading skeleton when content is empty and streaming', () => {
      const { container } = render(
        <AnswerStream content="" isStreaming={true} />
      )
      const skeleton = container.querySelector('[data-testid="loading-skeleton"]')
      expect(skeleton).toBeInTheDocument()
    })

    it('hides loading skeleton when content is present', () => {
      const { container } = render(
        <AnswerStream content="Some content" isStreaming={true} />
      )
      const skeleton = container.querySelector('[data-testid="loading-skeleton"]')
      expect(skeleton).not.toBeInTheDocument()
    })

    it('shows multiple skeleton lines for loading state', () => {
      const { container } = render(
        <AnswerStream content="" isStreaming={true} />
      )
      const skeletonLines = container.querySelectorAll('[data-slot="skeleton"]')
      expect(skeletonLines.length).toBeGreaterThanOrEqual(3)
    })
  })

  describe('error state', () => {
    it('shows error message when error is provided', () => {
      const error = new Error('Failed to generate response')
      render(
        <AnswerStream
          content=""
          isStreaming={false}
          error={error}
        />
      )
      expect(screen.getByText('Failed to generate response')).toBeInTheDocument()
    })

    it('shows retry button when error and onRetry are provided', () => {
      const error = new Error('Something went wrong')
      const onRetry = vi.fn()
      render(
        <AnswerStream
          content=""
          isStreaming={false}
          error={error}
          onRetry={onRetry}
        />
      )
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
    })

    it('calls onRetry when retry button is clicked', () => {
      const error = new Error('Something went wrong')
      const onRetry = vi.fn()
      render(
        <AnswerStream
          content=""
          isStreaming={false}
          error={error}
          onRetry={onRetry}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /retry/i }))
      expect(onRetry).toHaveBeenCalledTimes(1)
    })

    it('does not show retry button when onRetry is not provided', () => {
      const error = new Error('Something went wrong')
      render(
        <AnswerStream
          content=""
          isStreaming={false}
          error={error}
        />
      )
      expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument()
    })

    it('prioritizes error state over content display', () => {
      const error = new Error('Network error')
      render(
        <AnswerStream
          content="Some partial content"
          isStreaming={false}
          error={error}
        />
      )
      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('does not show error state when error is null', () => {
      render(
        <AnswerStream
          content="Some content"
          isStreaming={false}
          error={null}
        />
      )
      expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument()
    })
  })

  describe('text animation', () => {
    it('applies animation class to container when streaming', () => {
      const { container } = render(
        <AnswerStream content="Content" isStreaming={true} />
      )
      const answerContainer = container.querySelector('[data-testid="answer-stream"]')
      expect(answerContainer).toHaveClass('animate-in')
    })
  })

  describe('accessibility', () => {
    it('has appropriate aria-live attribute for streaming content', () => {
      const { container } = render(
        <AnswerStream content="Streaming..." isStreaming={true} />
      )
      const liveRegion = container.querySelector('[aria-live]')
      expect(liveRegion).toHaveAttribute('aria-live', 'polite')
    })

    it('has descriptive role for error state', () => {
      const error = new Error('Error occurred')
      render(
        <AnswerStream
          content=""
          isStreaming={false}
          error={error}
        />
      )
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
  })
})
