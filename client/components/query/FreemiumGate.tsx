'use client'

import { X } from 'lucide-react'

interface FreemiumGateProps {
  /** Number of citations hidden due to plan limits */
  hiddenCount: number
  /** Callback when user clicks upgrade */
  onUpgrade: () => void
  /** Optional callback when user dismisses the gate */
  onDismiss?: () => void
}

/**
 * Displays an upgrade prompt when there are hidden citations
 * due to free plan limitations.
 *
 * Shows:
 * - Count of hidden sources
 * - Premium features list
 * - Pricing information
 * - Upgrade button
 * - Optional dismiss button
 */
export function FreemiumGate({ hiddenCount, onUpgrade, onDismiss }: FreemiumGateProps) {
  return (
    <div
      className="freemium-gate relative rounded-lg border border-dashed border-primary/30 bg-primary/5 p-4 text-center"
      data-testid="freemium-gate"
    >
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          className="absolute top-2 right-2 p-1 rounded-full hover:bg-muted transition-colors"
          aria-label="Dismiss upgrade prompt"
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </button>
      )}

      <p className="text-sm font-medium text-foreground">
        {hiddenCount} more source{hiddenCount === 1 ? '' : 's'} available on Premium
      </p>

      <p className="text-xs text-muted-foreground mt-2">
        Full citations, EU AI Act coverage, and document generation
      </p>

      <p className="text-lg font-bold text-foreground mt-2">
        &euro;49<span className="text-xs font-normal text-muted-foreground">/month</span>
      </p>

      <button
        onClick={onUpgrade}
        className="mt-3 px-4 py-1.5 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
      >
        Upgrade to Premium
      </button>
    </div>
  )
}
