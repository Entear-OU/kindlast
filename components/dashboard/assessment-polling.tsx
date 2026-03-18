'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { AssessmentStatus } from './assessment-status'

interface AssessmentPollingProps {
  status: 'pending' | 'processing'
}

export function AssessmentPolling({ status }: AssessmentPollingProps) {
  const router = useRouter()

  useEffect(() => {
    // Poll every 5 seconds while assessment is pending/processing
    const interval = setInterval(() => {
      router.refresh()
    }, 5000)

    return () => clearInterval(interval)
  }, [router])

  return <AssessmentStatus status={status} />
}
