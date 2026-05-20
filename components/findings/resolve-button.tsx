'use client'

interface ResolveButtonProps {
  findingId: string
  isResolved: boolean
  onToggle: (findingId: string, resolved: boolean) => void
}

export function ResolveButton({ findingId, isResolved, onToggle }: ResolveButtonProps) {
  return (
    <button
      type="button"
      onClick={() => onToggle(findingId, !isResolved)}
      className={`mt-3 inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors ${
        isResolved
          ? 'border-yellow-200 bg-yellow-50 text-yellow-700 hover:bg-yellow-100'
          : 'border-green-200 bg-green-50 text-green-700 hover:bg-green-100'
      }`}
    >
      {isResolved ? 'Mark as Unresolved' : 'Mark as Resolved'}
    </button>
  )
}
