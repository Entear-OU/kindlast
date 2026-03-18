import { cn } from '@/lib/utils'

type RiskTier = 'unacceptable' | 'high' | 'limited' | 'minimal'

interface RiskTierBadgeProps {
  tier: RiskTier
  className?: string
}

const tierConfig: Record<RiskTier, { label: string; className: string }> = {
  unacceptable: {
    label: 'Unacceptable',
    className: 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400',
  },
  high: {
    label: 'High',
    className: 'bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-400',
  },
  limited: {
    label: 'Limited',
    className: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400',
  },
  minimal: {
    label: 'Minimal',
    className: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400',
  },
}

export function RiskTierBadge({ tier, className }: RiskTierBadgeProps) {
  const config = tierConfig[tier]

  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium',
        config.className,
        className
      )}
    >
      {config.label}
    </span>
  )
}
