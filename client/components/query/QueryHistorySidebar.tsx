'use client'

import { History, X } from 'lucide-react'

export interface QueryHistoryItem {
  id: string
  query: string
  timestamp: number
}

interface QueryHistorySidebarProps {
  history: QueryHistoryItem[]
  onSelectQuery: (query: string) => void
  onClearHistory: () => void
}

/**
 * Sidebar showing recent query history.
 * Allows users to re-run previous queries.
 */
export function QueryHistorySidebar({
  history,
  onSelectQuery,
  onClearHistory,
}: QueryHistorySidebarProps) {
  if (history.length === 0) {
    return (
      <div className="text-sm text-muted-foreground p-4 text-center">
        <History className="h-8 w-8 mx-auto mb-2 opacity-50" />
        <p>No recent queries</p>
        <p className="text-xs mt-1">Your recent questions will appear here</p>
      </div>
    )
  }

  return (
    <div className="space-y-2" data-testid="query-history-sidebar">
      <div className="flex items-center justify-between px-2">
        <h3 className="text-sm font-medium text-muted-foreground">Recent Queries</h3>
        <button
          onClick={onClearHistory}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          aria-label="Clear history"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      <ul className="space-y-1" role="list">
        {history.map((item) => (
          <li key={item.id}>
            <button
              onClick={() => onSelectQuery(item.query)}
              className="w-full text-left text-xs p-2 rounded-md hover:bg-accent transition-colors line-clamp-2"
              title={item.query}
            >
              {item.query}
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
