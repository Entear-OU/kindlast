import { Card, CardContent } from '@/components/ui/card'

interface AssessmentStatusProps {
  status: 'pending' | 'processing' | 'complete' | 'error'
}

const statusConfig: Record<
  string,
  { label: string; description: string; className: string }
> = {
  pending: {
    label: 'Pending',
    description: 'Your assessment is queued and will begin shortly.',
    className: 'text-yellow-600 bg-yellow-50 border-yellow-200',
  },
  processing: {
    label: 'Analyzing your compliance posture...',
    description: 'Our AI is reviewing your business profile. This may take a moment.',
    className: 'text-blue-600 bg-blue-50 border-blue-200',
  },
  complete: {
    label: 'Assessment Complete',
    description: 'Your compliance assessment is ready.',
    className: 'text-green-600 bg-green-50 border-green-200',
  },
  error: {
    label: 'Assessment Error',
    description: 'Something went wrong. Please try running the assessment again.',
    className: 'text-red-600 bg-red-50 border-red-200',
  },
}

export function AssessmentStatus({ status }: AssessmentStatusProps) {
  const config = statusConfig[status]

  return (
    <Card>
      <CardContent>
        <div
          data-status={status}
          className={`flex flex-col gap-2 rounded-lg border p-4 ${config.className}`}
        >
          <div className="flex items-center gap-2">
            {status === 'processing' && (
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
            )}
            <span className="text-sm font-semibold">{config.label}</span>
          </div>
          <p className="text-xs">{config.description}</p>
        </div>
      </CardContent>
    </Card>
  )
}
