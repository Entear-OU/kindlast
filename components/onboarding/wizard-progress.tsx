'use client'

import { cn } from '@/lib/utils'

const stepLabels = ['Company', 'Data', 'Compliance', 'AI Systems']

interface WizardProgressProps {
  currentStep: number
  totalSteps: number
}

export function WizardProgress({ currentStep, totalSteps }: WizardProgressProps) {
  const steps = stepLabels.slice(0, totalSteps)

  return (
    <div className="w-full">
      <ol className="flex items-center gap-2" role="list">
        {steps.map((label, index) => {
          const isCompleted = index < currentStep
          const isCurrent = index === currentStep

          return (
            <li
              key={label}
              role="listitem"
              aria-current={isCurrent ? 'step' : undefined}
              data-completed={isCompleted ? 'true' : undefined}
              className={cn(
                'flex flex-1 flex-col items-center gap-2',
              )}
            >
              <div className="flex w-full items-center gap-2">
                <div
                  className={cn(
                    'flex h-8 w-8 shrink-0 items-center justify-center rounded-full border-2 text-sm font-medium',
                    isCompleted && 'border-primary bg-primary text-primary-foreground',
                    isCurrent && 'border-primary text-primary',
                    !isCompleted && !isCurrent && 'border-muted-foreground/30 text-muted-foreground'
                  )}
                >
                  {isCompleted ? (
                    <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  ) : (
                    index + 1
                  )}
                </div>
                {index < steps.length - 1 && (
                  <div
                    className={cn(
                      'h-0.5 flex-1',
                      isCompleted ? 'bg-primary' : 'bg-muted-foreground/30'
                    )}
                  />
                )}
              </div>
              <span
                className={cn(
                  'text-xs font-medium',
                  isCurrent && 'text-primary',
                  isCompleted && 'text-primary',
                  !isCompleted && !isCurrent && 'text-muted-foreground'
                )}
              >
                {label}
              </span>
            </li>
          )
        })}
      </ol>
    </div>
  )
}
