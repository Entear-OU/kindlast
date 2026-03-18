'use client'

export default function DashboardError({
  error: _error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  void _error
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16">
      <h2 className="text-xl font-bold">Something went wrong</h2>
      <p className="text-sm text-muted-foreground">
        There was an error loading the dashboard. Please try again.
      </p>
      <button
        onClick={reset}
        className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
      >
        Try Again
      </button>
    </div>
  )
}
