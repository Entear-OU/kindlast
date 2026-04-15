'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { AssessmentStatus } from './assessment-status'
import { Button } from '@/components/ui/button'

interface AssessmentPollingProps {
  status: 'pending' | 'processing'
  profileId: string
}

export function AssessmentPolling({ status, profileId }: AssessmentPollingProps) {
  const router = useRouter()
  const [isRetrying, setIsRetrying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    // Poll every 5 seconds while assessment is pending/processing
    const interval = setInterval(() => {
      router.refresh()
    }, 5000)

    return () => clearInterval(interval)
  }, [router])

  const handleRetry = async () => {
    setIsRetrying(true)
    setError(null)

    try {
      const response = await fetch('/api/assess', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ profileId }),
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Failed to run assessment')
      }

      router.refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setIsRetrying(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <AssessmentStatus status={status} />
      {status === 'pending' && (
        <div className="flex flex-col items-center gap-2">
          <Button onClick={handleRetry} disabled={isRetrying} variant="outline">
            {isRetrying ? 'Running assessment...' : 'Retry Assessment'}
          </Button>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
      )}
    </div>
  )
}
