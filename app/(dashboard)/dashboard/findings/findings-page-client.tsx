'use client'

import { useState, useTransition } from 'react'
import { FindingCard } from '@/components/findings/finding-card'
import { FindingFilters } from '@/components/findings/finding-filters'
import { ResolveButton } from '@/components/findings/resolve-button'
import { toggleFindingResolved } from './actions'
import type { Finding } from '@/lib/types/database'

interface FindingsPageClientProps {
  findings: Finding[]
}

export function FindingsPageClient({ findings: initialFindings }: FindingsPageClientProps) {
  const [findings, setFindings] = useState(initialFindings)
  const [selectedSeverity, setSelectedSeverity] = useState('')
  const [selectedCategory, setSelectedCategory] = useState('')
  const [isPending, startTransition] = useTransition()

  const filteredFindings = findings.filter((finding) => {
    if (selectedSeverity && finding.severity !== selectedSeverity) return false
    if (selectedCategory && finding.category !== selectedCategory) return false
    return true
  })

  const handleToggleResolved = (findingId: string, resolved: boolean) => {
    // Optimistic update
    setFindings((prev) =>
      prev.map((f) =>
        f.id === findingId
          ? { ...f, is_resolved: resolved, resolved_at: resolved ? new Date().toISOString() : null }
          : f
      )
    )

    startTransition(async () => {
      try {
        await toggleFindingResolved(findingId, resolved)
      } catch {
        // Revert on error
        setFindings((prev) =>
          prev.map((f) =>
            f.id === findingId
              ? { ...f, is_resolved: !resolved, resolved_at: null }
              : f
          )
        )
      }
    })
  }

  return (
    <div className="flex flex-col gap-4">
      <FindingFilters
        selectedSeverity={selectedSeverity}
        selectedCategory={selectedCategory}
        onSeverityChange={setSelectedSeverity}
        onCategoryChange={setSelectedCategory}
      />

      {filteredFindings.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">
          No findings match the selected filters.
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          {filteredFindings.map((finding) => (
            <FindingCard key={finding.id} finding={finding}>
              <ResolveButton
                findingId={finding.id}
                isResolved={finding.is_resolved}
                onToggle={handleToggleResolved}
              />
            </FindingCard>
          ))}
        </div>
      )}

      {isPending && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/50">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
        </div>
      )}
    </div>
  )
}
