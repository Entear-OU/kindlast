'use client'

import { useState, useEffect, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import { useStreamingQuery } from '@/hooks/use-streaming-query'
import {
  QueryInput,
  AnswerStream,
  CitationList,
  QueryHistorySidebar,
  type QueryHistoryItem,
  type TopicFilter,
} from '@/components/query'
import { createPortalSession } from '@/lib/stripe/actions'

/**
 * Local storage key for query history
 */
const QUERY_HISTORY_KEY = 'kindlast_query_history'
const MAX_HISTORY_ITEMS = 5

/**
 * Plan limits for citations
 */
const PLAN_CITATION_LIMITS: Record<string, number> = {
  free: 3,
  premium: 10,
  api: 10,
}

/**
 * Default daily query limits by plan
 */
const PLAN_DAILY_LIMITS: Record<string, number> = {
  free: 5,
  premium: 100,
  api: 1000,
}

interface SettingsData {
  profile: {
    company_name: string
    country: string
    industry: string | null
    employee_count: number | null
  } | null
  subscription: {
    plan: string
    status: string
    current_period_end: string | null
  } | null
}

/**
 * Local storage key for daily query count
 */
const DAILY_QUERY_COUNT_KEY = 'kindlast_daily_query_count'

interface DailyQueryCount {
  date: string
  count: number
}

function getDailyQueryCount(): DailyQueryCount {
  if (typeof window === 'undefined') {
    return { date: new Date().toDateString(), count: 0 }
  }
  try {
    const stored = localStorage.getItem(DAILY_QUERY_COUNT_KEY)
    if (stored) {
      const parsed = JSON.parse(stored) as DailyQueryCount
      // Reset if it's a new day
      if (parsed.date !== new Date().toDateString()) {
        return { date: new Date().toDateString(), count: 0 }
      }
      return parsed
    }
  } catch {
    // Ignore parse errors
  }
  return { date: new Date().toDateString(), count: 0 }
}

function incrementDailyQueryCount(): DailyQueryCount {
  const current = getDailyQueryCount()
  const updated = {
    date: new Date().toDateString(),
    count: current.date === new Date().toDateString() ? current.count + 1 : 1,
  }
  localStorage.setItem(DAILY_QUERY_COUNT_KEY, JSON.stringify(updated))
  return updated
}

export default function QueryPage() {
  const router = useRouter()
  const [queryValue, setQueryValue] = useState('')
  const [currentTopic, setCurrentTopic] = useState<TopicFilter>('gdpr')
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [settingsLoading, setSettingsLoading] = useState(true)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [queryHistory, setQueryHistory] = useState<QueryHistoryItem[]>([])
  const [dailyQueryCount, setDailyQueryCount] = useState<DailyQueryCount>({ date: '', count: 0 })

  // Initialize the streaming query hook with topic
  const {
    data: answer,
    citations,
    isStreaming,
    error,
    metadata,
    executeQuery,
    reset,
  } = useStreamingQuery({ topic: currentTopic })

  // Load settings on mount
  useEffect(() => {
    async function loadSettings() {
      try {
        const res = await fetch('/api/settings')
        if (res.status === 401) {
          router.push('/login')
          return
        }
        if (!res.ok) {
          setSettingsError('Failed to load settings')
          return
        }
        const data = await res.json()
        setSettings(data)
      } catch {
        setSettingsError('Failed to load settings')
      } finally {
        setSettingsLoading(false)
      }
    }
    loadSettings()
  }, [router])

  // Load query history from localStorage
  useEffect(() => {
    if (typeof window === 'undefined') return
    try {
      const stored = localStorage.getItem(QUERY_HISTORY_KEY)
      if (stored) {
        setQueryHistory(JSON.parse(stored))
      }
    } catch {
      // Ignore parse errors
    }
    // Load daily count
    setDailyQueryCount(getDailyQueryCount())
  }, [])

  // Save query history to localStorage
  const saveQueryToHistory = useCallback((query: string) => {
    const newItem: QueryHistoryItem = {
      id: crypto.randomUUID(),
      query,
      timestamp: Date.now(),
    }
    setQueryHistory((prev) => {
      const filtered = prev.filter((item) => item.query !== query)
      const updated = [newItem, ...filtered].slice(0, MAX_HISTORY_ITEMS)
      localStorage.setItem(QUERY_HISTORY_KEY, JSON.stringify(updated))
      return updated
    })
  }, [])

  const clearHistory = useCallback(() => {
    setQueryHistory([])
    localStorage.removeItem(QUERY_HISTORY_KEY)
  }, [])

  // Get plan info
  const plan = settings?.subscription?.plan || 'free'
  const citationLimit = PLAN_CITATION_LIMITS[plan] || 3
  const dailyLimit = PLAN_DAILY_LIMITS[plan] || 5
  const remainingQueries = Math.max(0, dailyLimit - dailyQueryCount.count)

  // Check if rate limited
  const isRateLimited = remainingQueries <= 0

  // Handle query submission
  const handleSubmit = useCallback(
    (query: string, topic: TopicFilter) => {
      if (isRateLimited) return
      setCurrentTopic(topic)
      reset()
      saveQueryToHistory(query)
      setDailyQueryCount(incrementDailyQueryCount())
      executeQuery(query)
    },
    [executeQuery, reset, saveQueryToHistory, isRateLimited]
  )

  // Handle selecting a query from history
  const handleSelectFromHistory = useCallback(
    (query: string) => {
      setQueryValue(query)
      handleSubmit(query, currentTopic)
    },
    [handleSubmit, currentTopic]
  )

  // Handle upgrade to premium
  const handleUpgrade = async () => {
    try {
      const result = await createPortalSession()
      if (result.url) {
        window.location.href = result.url
      } else {
        // If no portal URL, redirect to pricing page
        router.push('/pricing')
      }
    } catch {
      // Fallback to pricing page
      router.push('/pricing')
    }
  }

  const isDone = !isStreaming && answer.length > 0
  const hasError = error !== null

  return (
    <div className="flex flex-col lg:flex-row min-h-full">
      {/* Main content area */}
      <div className="flex-1 px-4 py-6 lg:px-8">
        <div className="max-w-3xl mx-auto space-y-6">
          {/* Header */}
          <div>
            <h1 className="text-2xl font-bold">Compliance Q&A</h1>
            <p className="text-muted-foreground">
              Ask questions about GDPR and EU AI Act compliance
            </p>
          </div>

          {/* Plan and rate limit info */}
          <div className="flex flex-wrap items-center gap-4 text-sm">
            {!settingsLoading && (
              <>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Plan:</span>
                  <span className="font-medium capitalize">{plan}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Queries today:</span>
                  <span className="font-medium">
                    {dailyQueryCount.count} / {dailyLimit}
                  </span>
                </div>
                {isRateLimited && (
                  <span className="text-destructive font-medium">
                    Daily limit reached
                  </span>
                )}
              </>
            )}
          </div>

          {/* Settings error */}
          {settingsError && (
            <div className="rounded-lg bg-destructive/10 border border-destructive/20 p-3 text-sm text-destructive">
              {settingsError}
            </div>
          )}

          {/* Rate limit warning */}
          {isRateLimited && (
            <div className="rounded-lg border border-dashed p-4 text-center">
              <p className="text-sm font-medium">Daily query limit reached</p>
              <p className="text-xs text-muted-foreground mt-1">
                Upgrade to Premium for more queries per day
              </p>
              <button
                onClick={handleUpgrade}
                className="mt-3 px-4 py-1.5 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                Upgrade to Premium
              </button>
            </div>
          )}

          {/* Query input */}
          <QueryInput
            value={queryValue}
            onChange={setQueryValue}
            onSubmit={handleSubmit}
            isLoading={isStreaming}
            disabled={settingsLoading || isRateLimited}
          />

          {/* Two-column layout for answer and citations */}
          {(isStreaming || isDone || hasError) && (
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
              {/* Answer column */}
              <div className="lg:col-span-2">
                <AnswerStream
                  content={answer}
                  isStreaming={isStreaming}
                  confidence={metadata?.maxRelevance}
                  error={error ? new Error(error) : null}
                  onRetry={() => {
                    reset()
                    executeQuery(queryValue)
                  }}
                />
                {metadata && isDone && (
                  <p className="text-xs text-muted-foreground mt-2">
                    {citations.length} source{citations.length !== 1 ? 's' : ''} found
                  </p>
                )}
              </div>

              {/* Citations column - stacked on mobile, side on desktop */}
              {isDone && citations.length > 0 && (
                <div className="lg:col-span-1">
                  <CitationList
                    citations={citations}
                    planLimit={citationLimit}
                    onUpgrade={handleUpgrade}
                  />
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Query history sidebar - hidden on mobile, shown on desktop */}
      <aside className="hidden lg:block w-64 border-l bg-card p-4">
        <QueryHistorySidebar
          history={queryHistory}
          onSelectQuery={handleSelectFromHistory}
          onClearHistory={clearHistory}
        />
      </aside>
    </div>
  )
}
