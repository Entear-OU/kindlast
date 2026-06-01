'use client'

import Link from 'next/link'
import { useEffect } from 'react'

import {
  trackUpgradeConverted,
  trackUpgradePromptShown,
  type UpgradeSource,
} from '@/lib/analytics/track'
import { upgradeHref } from '@/lib/billing/upgrade-link'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

/**
 * The "Upgrade to act" modal (ENT-83).
 *
 * Raised the moment a Free user reaches for a Pro-only action (e.g. tapping
 * Approve, which never fires the Executor on Free). It explains what Pro unlocks
 * and reassures that nothing is lost, then routes to checkout. The default copy
 * is the finding-approval message; callers can override it to reuse the modal
 * for other conversion moments.
 *
 * Tracking: `upgrade_prompt_shown` fires when the modal opens, `upgrade_prompt_
 * converted` when the founder taps the CTA — keyed by `source` so each entry
 * point is measurable.
 */

export const APPROVE_UPGRADE_TITLE = 'Upgrade to act'
export const APPROVE_UPGRADE_DESCRIPTION =
  'Pro unlocks one-tap actions. Your finding is still here, waiting.'

export function UpgradeDialog({
  open,
  onOpenChange,
  source,
  returnTo,
  title = APPROVE_UPGRADE_TITLE,
  description = APPROVE_UPGRADE_DESCRIPTION,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  source: UpgradeSource
  /** Where checkout should return the user on success (the path they were on). */
  returnTo?: string
  title?: string
  description?: string
}) {
  useEffect(() => {
    if (open) trackUpgradePromptShown({ source })
  }, [open, source])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose render={<Button variant="outline" />}>Not now</DialogClose>
          <Link
            href={upgradeHref(returnTo)}
            onClick={() => trackUpgradeConverted({ source })}
            className={cn(buttonVariants())}
          >
            Upgrade to Pro
          </Link>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
