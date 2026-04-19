'use client'

import { useState, useCallback, KeyboardEvent } from 'react'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { Loader2, X } from 'lucide-react'

export type TopicFilter = 'gdpr' | 'ai_act' | 'both'

const TOPIC_OPTIONS: { value: TopicFilter; label: string }[] = [
  { value: 'gdpr', label: 'GDPR' },
  { value: 'ai_act', label: 'AI Act' },
  { value: 'both', label: 'Both' },
]

const EXAMPLE_QUESTIONS = [
  'What are the lawful bases for processing under GDPR?',
  'When is a DPIA required?',
  'What are the AI Act risk categories?',
  'How do I respond to a data subject access request?',
]

const MAX_CHARS = 2000
const NEAR_LIMIT_THRESHOLD = 1800

export interface QueryInputProps {
  value: string
  onChange: (value: string) => void
  onSubmit: (query: string, topic: TopicFilter) => void
  isLoading?: boolean
  disabled?: boolean
  placeholder?: string
}

export function QueryInput({
  value,
  onChange,
  onSubmit,
  isLoading = false,
  disabled = false,
  placeholder = 'Ask a GDPR or EU AI Act compliance question...',
}: QueryInputProps) {
  const [topic, setTopic] = useState<TopicFilter>('gdpr')

  const isDisabled = disabled || isLoading
  const canSubmit = value.trim().length > 0 && !isDisabled
  const showExamples = !value && !isLoading
  const charCount = value.length
  const isNearLimit = charCount >= NEAR_LIMIT_THRESHOLD

  const handleSubmit = useCallback(() => {
    const trimmedQuery = value.trim()
    if (trimmedQuery && !isDisabled) {
      onSubmit(trimmedQuery, topic)
    }
  }, [value, topic, isDisabled, onSubmit])

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      // Submit on Cmd+Enter (Mac) or Ctrl+Enter (Windows/Linux)
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        handleSubmit()
      }
    },
    [handleSubmit]
  )

  const handleExampleClick = useCallback(
    (example: string) => {
      onChange(example)
    },
    [onChange]
  )

  const handleClear = useCallback(() => {
    onChange('')
  }, [onChange])

  const handleTopicChange = useCallback((newTopic: TopicFilter) => {
    setTopic(newTopic)
  }, [])

  return (
    <div className="space-y-4">
      {/* Main input container */}
      <div className="relative rounded-xl border bg-background focus-within:ring-1 focus-within:ring-primary">
        <label className="sr-only" htmlFor="query-input">
          Compliance question
        </label>
        <Textarea
          id="query-input"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          disabled={isDisabled}
          className={cn(
            'min-h-[120px] w-full resize-none rounded-xl border-0 bg-transparent px-4 pt-4 pb-16',
            'text-base focus:outline-none focus-visible:ring-0 focus-visible:border-0',
            'placeholder:text-muted-foreground'
          )}
          maxLength={MAX_CHARS}
          aria-label="Compliance question"
        />

        {/* Bottom bar with topic filters, char count, and buttons */}
        <div className="absolute bottom-3 left-3 right-3 flex items-center justify-between gap-2">
          {/* Topic filter buttons */}
          <div className="flex gap-1" role="group" aria-label="Topic filter">
            {TOPIC_OPTIONS.map((option) => (
              <button
                key={option.value}
                type="button"
                onClick={() => handleTopicChange(option.value)}
                disabled={isDisabled}
                data-selected={topic === option.value}
                className={cn(
                  'rounded-full border px-3 py-1 text-xs font-medium transition-colors',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2',
                  'disabled:opacity-50 disabled:cursor-not-allowed',
                  topic === option.value
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'border-input text-muted-foreground hover:border-primary hover:text-foreground'
                )}
              >
                {option.label}
              </button>
            ))}
          </div>

          {/* Right side: character count, clear, and submit */}
          <div className="flex items-center gap-2">
            {/* Character count */}
            <span
              data-near-limit={isNearLimit}
              className={cn(
                'text-xs tabular-nums',
                isNearLimit ? 'text-amber-600' : 'text-muted-foreground'
              )}
            >
              {charCount}/{MAX_CHARS}
            </span>

            {/* Clear button - only show when there's text */}
            {value && (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={handleClear}
                disabled={isLoading}
                aria-label="Clear"
              >
                <X className="size-4" />
              </Button>
            )}

            {/* Submit button */}
            <Button
              type="button"
              onClick={handleSubmit}
              disabled={!canSubmit}
              size="sm"
              className="gap-1.5"
            >
              {isLoading ? (
                <>
                  <Loader2 className="size-3.5 animate-spin" />
                  Asking...
                </>
              ) : (
                'Ask'
              )}
            </Button>
          </div>
        </div>
      </div>

      {/* Keyboard shortcut hint */}
      <p className="text-xs text-muted-foreground text-center">
        Press <kbd className="rounded border bg-muted px-1.5 py-0.5 text-xs font-mono">Cmd+Enter</kbd> or{' '}
        <kbd className="rounded border bg-muted px-1.5 py-0.5 text-xs font-mono">Ctrl+Enter</kbd> to submit
      </p>

      {/* Example questions */}
      {showExamples && (
        <div className="space-y-2">
          <p className="text-sm font-medium text-muted-foreground">Try an example:</p>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {EXAMPLE_QUESTIONS.map((question) => (
              <button
                key={question}
                type="button"
                onClick={() => handleExampleClick(question)}
                className={cn(
                  'rounded-lg border border-input bg-background p-3 text-left text-sm',
                  'text-muted-foreground transition-colors',
                  'hover:border-primary hover:text-foreground',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2'
                )}
              >
                {question}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
