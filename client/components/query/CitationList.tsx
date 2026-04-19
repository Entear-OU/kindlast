'use client'

import { useState, useEffect } from 'react'
import type { Citation } from '@/lib/api/types'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { FreemiumGate } from './FreemiumGate'

/**
 * Truncation threshold for excerpts (in characters)
 */
const EXCERPT_TRUNCATE_LENGTH = 200

interface CitationListProps {
  citations: Citation[]
  planLimit: number // 3 for free, 10 for premium
  onUpgrade: () => void
}

/**
 * Determines the source tier based on the source name/URL.
 * Tier 1: Primary EU legislation (EUR-Lex)
 * Tier 2: EU bodies guidance (EDPB, EU institutions)
 * Tier 3: National DPAs and other sources
 */
function getSourceTier(source: string, url: string): 1 | 2 | 3 {
  const lowerSource = source.toLowerCase()
  const lowerUrl = url.toLowerCase()

  // Tier 1: Primary EU legislation
  if (
    lowerSource.includes('eur-lex') ||
    lowerUrl.includes('eur-lex.europa.eu') ||
    lowerSource.includes('official journal')
  ) {
    return 1
  }

  // Tier 2: EU bodies guidance
  if (
    lowerSource.includes('edpb') ||
    lowerUrl.includes('edpb.europa.eu') ||
    lowerSource.includes('european data protection board') ||
    lowerSource.includes('ec.europa.eu') ||
    lowerUrl.includes('ec.europa.eu')
  ) {
    return 2
  }

  // Tier 3: Everything else (national DPAs, etc.)
  return 3
}

interface CitationItemProps {
  citation: Citation
  index: number
  isHighlighted: boolean
}

/**
 * Individual citation card component
 */
function CitationItem({ citation, index, isHighlighted }: CitationItemProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const tier = getSourceTier(citation.source, citation.url)
  const shouldTruncate = citation.excerpt && citation.excerpt.length > EXCERPT_TRUNCATE_LENGTH

  const displayExcerpt = isExpanded || !shouldTruncate
    ? citation.excerpt
    : citation.excerpt?.slice(0, EXCERPT_TRUNCATE_LENGTH)

  const tierVariant = tier === 1 ? 'default' : tier === 2 ? 'secondary' : 'outline'

  return (
    <div
      id={`citation-${index}`}
      className={cn(
        'citation-card rounded-lg border p-3 text-sm transition-all',
        isHighlighted && 'ring-2 ring-primary ring-offset-2'
      )}
      data-testid={`citation-card-${index}`}
    >
      <div className="flex items-start gap-2">
        <span
          data-testid={`citation-number-${index}`}
          className="citation-number flex-shrink-0 text-xs font-medium rounded-full bg-primary/10 text-primary w-5 h-5 flex items-center justify-center"
        >
          {index}
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-sm truncate">
              {citation.title || getHostname(citation.url)}
            </span>
            <Badge variant={tierVariant} className="text-[10px] px-1.5 py-0">
              Tier {tier}
            </Badge>
          </div>
          {citation.source && (
            <div className="text-xs text-muted-foreground mt-0.5">
              {citation.source}
            </div>
          )}
          <a
            href={citation.url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-blue-600 hover:underline mt-1 inline-block truncate max-w-full"
          >
            {citation.url}
          </a>
          {citation.excerpt && (
            <>
              <p className="text-xs text-muted-foreground mt-1">
                {displayExcerpt}
                {shouldTruncate && !isExpanded && '...'}
              </p>
              {shouldTruncate && (
                <button
                  type="button"
                  onClick={() => setIsExpanded(!isExpanded)}
                  className="text-xs text-primary hover:underline mt-1"
                >
                  {isExpanded ? 'Show less' : 'Show more'}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * Renders a list of citation cards with source information.
 * Shows a freemium gate if there are more citations than the plan allows.
 *
 * Features:
 * - ID anchors for superscript links (id="citation-1", "citation-2", etc.)
 * - Citation numbers, source titles, and URLs
 * - Excerpt truncation with "show more" expansion
 * - Tier badges (Tier 1, Tier 2, Tier 3)
 * - Highlights currently focused citation (via URL hash)
 * - Freemium gate when citations are limited
 */
export function CitationList({ citations, planLimit, onUpgrade }: CitationListProps) {
  const [highlightedCitation, setHighlightedCitation] = useState<number | null>(null)

  // Handle hash-based citation highlighting
  useEffect(() => {
    const handleHashChange = () => {
      const hash = typeof window !== 'undefined' ? window.location.hash : ''
      if (hash.startsWith('#citation-')) {
        const num = parseInt(hash.replace('#citation-', ''), 10)
        if (!isNaN(num)) {
          setHighlightedCitation(num)
          // Auto-clear highlight after 3 seconds
          setTimeout(() => setHighlightedCitation(null), 3000)
        }
      }
    }

    // Check initial hash
    handleHashChange()

    // Listen for hash changes
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [])

  if (citations.length === 0) {
    return null
  }

  const visible = citations.slice(0, planLimit)
  const hidden = citations.length - visible.length

  return (
    <div className="citations-container space-y-3" data-testid="citation-list">
      <h3 className="text-sm font-medium text-muted-foreground">
        Sources ({citations.length})
      </h3>

      {visible.map((citation, index) => {
        const citationNumber = index + 1
        return (
          <CitationItem
            key={`${citation.source}-${index}`}
            citation={citation}
            index={citationNumber}
            isHighlighted={highlightedCitation === citationNumber}
          />
        )
      })}

      {hidden > 0 && (
        <FreemiumGate hiddenCount={hidden} onUpgrade={onUpgrade} />
      )}
    </div>
  )
}

/**
 * Extract hostname from URL for display when title is not available
 */
function getHostname(url: string): string {
  try {
    return new URL(url).hostname
  } catch {
    return url
  }
}
