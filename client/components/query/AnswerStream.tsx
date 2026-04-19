'use client'

import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import { AlertTriangle, RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

interface AnswerStreamProps {
  /** Accumulated markdown text content */
  content: string
  /** Whether streaming is currently active */
  isStreaming: boolean
  /** Confidence score (0-1), shows warning when < 0.72 */
  confidence?: number
  /** Error state to display */
  error?: Error | null
  /** Callback for retry action */
  onRetry?: () => void
}

/**
 * AnswerStream component displays streaming markdown content with:
 * - Proper markdown rendering via react-markdown
 * - Blinking cursor while streaming
 * - Inline citation superscripts [1], [2] linking to #citation-N anchors
 * - Low confidence warning banner (< 0.72)
 * - Loading skeleton when waiting for first chunk
 * - Error state with retry button
 */
export function AnswerStream({
  content,
  isStreaming,
  confidence,
  error,
  onRetry,
}: AnswerStreamProps) {
  // Custom components for react-markdown to handle citations
  const markdownComponents = useMemo(
    () => ({
      // Process paragraph children to convert [N] to citation links
      p: ({ children, ...props }: React.ComponentProps<'p'>) => {
        const processedChildren = processChildren(children)
        return <p {...props}>{processedChildren}</p>
      },
      // Process list item children
      li: ({ children, ...props }: React.ComponentProps<'li'>) => {
        const processedChildren = processChildren(children)
        return <li {...props}>{processedChildren}</li>
      },
    }),
    []
  )

  // Show error state
  if (error) {
    return (
      <div
        role="alert"
        className="rounded-lg border border-destructive/20 bg-destructive/10 p-4"
      >
        <div className="flex items-start gap-3">
          <AlertTriangle className="mt-0.5 h-5 w-5 flex-shrink-0 text-destructive" />
          <div className="flex-1">
            <p className="text-sm font-medium text-destructive">
              {error.message}
            </p>
            {onRetry && (
              <Button
                variant="outline"
                size="sm"
                onClick={onRetry}
                className="mt-3"
              >
                <RefreshCw className="mr-2 h-4 w-4" />
                Retry
              </Button>
            )}
          </div>
        </div>
      </div>
    )
  }

  // Show loading skeleton when waiting for first chunk
  if (!content && isStreaming) {
    return (
      <div data-testid="loading-skeleton" className="space-y-3">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-[90%]" />
        <Skeleton className="h-4 w-[75%]" />
        <Skeleton className="h-4 w-[85%]" />
      </div>
    )
  }

  const showLowConfidenceWarning =
    confidence !== undefined && confidence < 0.72

  return (
    <div
      data-testid="answer-stream"
      className={cn(
        'space-y-4',
        isStreaming && 'animate-in fade-in-0 duration-300'
      )}
    >
      {showLowConfidenceWarning && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800 dark:bg-amber-950">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 flex-shrink-0 text-amber-600 dark:text-amber-400" />
            <p className="text-sm text-amber-800 dark:text-amber-200">
              Limited source material found. Consider consulting a qualified DPO
              for your specific situation.
            </p>
          </div>
        </div>
      )}

      <div
        data-testid="answer-content"
        aria-live="polite"
        className="prose prose-sm max-w-none dark:prose-invert"
      >
        <ReactMarkdown components={markdownComponents}>
          {content}
        </ReactMarkdown>
        {isStreaming && (
          <span
            data-testid="streaming-cursor"
            className="ml-0.5 inline-block h-4 w-0.5 animate-pulse bg-current"
            aria-hidden="true"
          />
        )}
      </div>
    </div>
  )
}

/**
 * Process React children to convert [N] patterns to citation links
 */
function processChildren(children: React.ReactNode): React.ReactNode {
  if (typeof children === 'string') {
    return processCitationText(children)
  }

  if (Array.isArray(children)) {
    return children.map((child, index) => {
      if (typeof child === 'string') {
        return <span key={index}>{processCitationText(child)}</span>
      }
      return child
    })
  }

  return children
}

/**
 * Convert citation markers [N] in text to clickable superscript links
 */
function processCitationText(text: string): React.ReactNode {
  const parts = text.split(/(\[\d+\])/g)

  if (parts.length === 1) {
    return text
  }

  return parts.map((part, index) => {
    const match = part.match(/^\[(\d+)\]$/)
    if (match) {
      const num = match[1]
      return (
        <sup key={index}>
          <a
            href={`#citation-${num}`}
            className="citation-link text-primary hover:underline"
          >
            [{num}]
          </a>
        </sup>
      )
    }
    return part
  })
}
