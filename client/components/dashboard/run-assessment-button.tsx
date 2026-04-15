'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'
import { Button } from '@/components/ui/button'

interface RunAssessmentButtonProps {
  profileId: string
}

export function RunAssessmentButton({ profileId }: RunAssessmentButtonProps) {
  const [isRunning, setIsRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const router = useRouter()

  const handleRun = async () => {
    setIsRunning(true)
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
      setIsRunning(false)
    }
  }

  return (
    <div className="flex flex-col items-center gap-2">
      <Button onClick={handleRun} disabled={isRunning} size="lg">
        {isRunning ? 'Analyzing your compliance posture...' : 'Run GDPR Assessment'}
      </Button>
      {error && (
        <p className="text-sm text-destructive">{error}</p>
      )}
    </div>
  )
}
